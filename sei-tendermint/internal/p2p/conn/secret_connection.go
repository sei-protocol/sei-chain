package conn

import (
	"bytes"
	"context"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"

	"github.com/oasisprotocol/curve25519-voi/primitives/merlin"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

var errAEAD = errors.New("decoding failed")
var errDH = errors.New("DH secret failure")

// 4 + 1024 == 1028 total frame size
const (
	dataSizeLen   = 4
	dataSizeMax   = 1024
	frameSize     = dataSizeLen + dataSizeMax
	aeadOverhead  = chacha20poly1305.Overhead
	aeadNonceSize = chacha20poly1305.NonceSize

	labelEphemeralLowerPublicKey = "EPHEMERAL_LOWER_PUBLIC_KEY"
	labelEphemeralUpperPublicKey = "EPHEMERAL_UPPER_PUBLIC_KEY"
	labelDHSecret                = "DH_SECRET"
	labelSecretConnectionMac     = "SECRET_CONNECTION_MAC"
)

type asyncMutex[T any] struct {
	mu chan struct{}
	v  T
}

func newAsyncMutex[T any](v T) asyncMutex[T] {
	return asyncMutex[T]{make(chan struct{}, 1), v}
}

func (m *asyncMutex[T]) Lock(ctx context.Context, yield func(T) error) error {
	if err := utils.Send(ctx, m.mu, struct{}{}); err != nil {
		return err
	}
	defer func() { <-m.mu }()
	return yield(m.v)
}

var secretConnKeyAndChallengeGen = []byte("TENDERMINT_SECRET_CONNECTION_KEY_AND_CHALLENGE_GEN")

type sendState struct {
	cipher cipher.AEAD
	frame  []byte
	data   []byte
	nonce  uint64
}

type recvState struct {
	cipher cipher.AEAD
	frame  []byte
	data   []byte
	nonce  uint64
}

func newSendState(cipher cipher.AEAD) *sendState {
	frame := make([]byte, frameSize)
	return &sendState{
		cipher: cipher,
		frame:  frame,
		data:   frame[dataSizeLen:dataSizeLen],
	}
}

func newRecvState(cipher cipher.AEAD) *recvState {
	return &recvState{
		cipher: cipher,
		frame:  make([]byte, frameSize),
	}
}

var _ Conn = (*SecretConnection)(nil)

type Challenge [32]byte

// SecretConnection implements Conn.
// It is an implementation of the STS protocol.
// See https://github.com/tendermint/tendermint/blob/0.1/docs/sts-final.pdf for
// details on the protocol.
//
// Consumers of the SecretConnection are responsible for authenticating
// the remote peer's pubkey against known information, like a nodeID.
// Otherwise they are vulnerable to MITM.
// (TODO(ismail): see also https://github.com/tendermint/tendermint/issues/3010)
type SecretConnection struct {
	conn      Conn
	challenge Challenge
	recvState asyncMutex[*recvState]
	sendState asyncMutex[*sendState]
}

func (sc *SecretConnection) Challenge() Challenge { return sc.challenge }

func newSecretConnection(conn Conn, loc ephSecret, rem ephPublic) (*SecretConnection, error) {
	pubs := utils.Slice(loc.public, rem)
	if bytes.Compare(pubs[0][:], pubs[1][:]) > 0 {
		pubs[0], pubs[1] = pubs[1], pubs[0]
	}
	transcript := merlin.NewTranscript("TENDERMINT_SECRET_CONNECTION_TRANSCRIPT_HASH")
	transcript.AppendMessage(labelEphemeralLowerPublicKey, pubs[0][:])
	transcript.AppendMessage(labelEphemeralUpperPublicKey, pubs[1][:])
	dh, err := loc.DhSecret(rem)
	if err != nil {
		return nil, err
	}
	transcript.AppendMessage(labelDHSecret, dh[:])
	var challenge Challenge
	transcript.ExtractBytes(challenge[:], labelSecretConnectionMac)

	// Generate the secret used for receiving, sending, challenge via HKDF-SHA2
	// on the transcript state (which itself also uses HKDF-SHA2 to derive a key
	// from the dhSecret).
	aead := dh.AeadSecrets(loc.public == pubs[0])
	return &SecretConnection{
		conn:      conn,
		challenge: challenge,
		recvState: newAsyncMutex(newRecvState(aead.recv.Cipher())),
		sendState: newAsyncMutex(newSendState(aead.send.Cipher())),
	}, nil
}

// MakeSecretConnection performs handshake and returns an encrypted SecretConnection.
// To authenticate the secret connection, you need to sign the Challenge() and exchange the signatures
// with the peer. See docs/sts-final.pdf for more information.
func MakeSecretConnection(ctx context.Context, conn Conn) (*SecretConnection, error) {
	// Write local ephemeral pubkey and receive one too.
	// NOTE: every 32-byte string is accepted as a Curve25519 public key (see
	// DJB's Curve25519 paper: http://cr.yp.to/ecdh/curve25519-20060209.pdf)
	sc, err := scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*SecretConnection, error) {
		// Generate ephemeral key for perfect forward secrecy.
		loc := genEphKey()
		s.Spawn(func() error {
			prefaceMsg := &pb.Preface{StsPublicKey: loc.public[:]}
			if err := WriteSizedMsg(ctx, conn, protoutils.Marshal(prefaceMsg)); err != nil {
				return err
			}
			return conn.Flush(ctx)
		})
		prefaceBytes, err := ReadSizedMsg(ctx, conn, 1024)
		if err != nil {
			return nil, fmt.Errorf("ReadSizedMsg(): %w", err)
		}
		prefaceMsg, err := protoutils.Unmarshal[*pb.Preface](prefaceBytes)
		if err != nil {
			return nil, fmt.Errorf("Unmarshal(): %w", err)
		}
		if len(prefaceMsg.StsPublicKey) != len(ephPublic{}) {
			return nil, errors.New("bad ephemeral key size")
		}
		return newSecretConnection(conn, loc, ephPublic(prefaceMsg.StsPublicKey))
	})
	if err != nil {
		return nil, err
	}
	return sc, nil
}

// Writes encrypted frames of `totalFrameSize + aeadSizeOverhead`.
func (sc *SecretConnection) Write(ctx context.Context, data []byte) error {
	return sc.sendState.Lock(ctx, func(sendState *sendState) error {
		n := 0
		for {
			chunk := min(len(data), cap(sendState.data)-len(sendState.data))
			sendState.data = append(sendState.data, data[:chunk]...)
			n += chunk
			data = data[chunk:]
			if len(data) == 0 {
				return nil
			}
			if err := sc.flush(ctx, sendState); err != nil {
				return err
			}
		}
	})
}

func (sc *SecretConnection) flush(ctx context.Context, sendState *sendState) error {
	if len(sendState.data) == 0 {
		return nil
	}
	binary.LittleEndian.PutUint32(sendState.frame, uint32(len(sendState.data)))
	if sendState.nonce == math.MaxUint64 {
		return fmt.Errorf("nonce overflow")
	}
	var nonce [aeadNonceSize]byte
	binary.LittleEndian.PutUint64(nonce[4:], sendState.nonce)
	sendState.nonce += 1
	// We use a predeclared stack-allocated buffer, to prevent Seal from doing heap allocation.
	// I'm not surre whether this optimization is needed though.
	var sealedFrame [frameSize + aeadOverhead]byte
	err := sc.conn.Write(ctx, sendState.cipher.Seal(sealedFrame[:0], nonce[:], sendState.frame[:], nil))
	// Zeroize the the frame to avoid resending data from the previous frame.
	// Security-wise it doesn't make any difference, it is here just to avoid people raising concerns.
	clear(sendState.frame)
	sendState.data = sendState.frame[dataSizeLen:dataSizeLen]
	return err
}

func (sc *SecretConnection) Read(ctx context.Context, data []byte) error {
	return sc.recvState.Lock(ctx, func(recvState *recvState) error {
		for len(data) > 0 {
			if len(recvState.data) == 0 {
				var sealedFrame [frameSize + aeadOverhead]byte
				if err := sc.conn.Read(ctx, sealedFrame[:]); err != nil {
					return err
				}
				if recvState.nonce == math.MaxUint64 {
					return fmt.Errorf("nonce overflow")
				}
				var nonce [aeadNonceSize]byte
				binary.LittleEndian.PutUint64(nonce[4:], recvState.nonce)
				recvState.nonce += 1
				if _, err := recvState.cipher.Open(recvState.frame[:0], nonce[:], sealedFrame[:], nil); err != nil {
					return fmt.Errorf("%w: %v", errAEAD, err)
				}
				dataSize := binary.LittleEndian.Uint32(recvState.frame)
				if dataSize > dataSizeMax {
					return errors.New("dataSize is greater than dataSizeMax")
				}
				recvState.data = recvState.frame[dataSizeLen : dataSizeLen+dataSize]
			}
			n := copy(data, recvState.data)
			data = data[n:]
			recvState.data = recvState.data[n:]
		}
		return nil
	})
}

// Implements Conn
func (sc *SecretConnection) Flush(ctx context.Context) error {
	return sc.sendState.Lock(ctx, func(sendState *sendState) error {
		return sc.flush(ctx, sendState)
	})
}

func (sc *SecretConnection) LocalAddr() netip.AddrPort  { return sc.conn.LocalAddr() }
func (sc *SecretConnection) RemoteAddr() netip.AddrPort { return sc.conn.RemoteAddr() }
func (sc *SecretConnection) Close()                     { sc.conn.Close() }

type ephPublic [32]byte

type ephSecret struct {
	secret [32]byte
	public ephPublic
}

func genEphKey() ephSecret {
	// TODO: Probably not a problem but ask Tony: different from the rust implementation (uses x25519-dalek),
	// we do not "clamp" the private key scalar:
	// see: https://github.com/dalek-cryptography/x25519-dalek/blob/34676d336049df2bba763cc076a75e47ae1f170f/src/x25519.rs#L56-L74
	public, secret, err := box.GenerateKey(crand.Reader)
	if err != nil {
		panic(fmt.Errorf("Could not generate ephemeral key-pair: %w", err))
	}
	return ephSecret{
		secret: *secret,
		public: *public,
	}
}

type dhSecret [32]byte
type aeadSecret [chacha20poly1305.KeySize]byte

type aeadSecrets struct {
	send aeadSecret
	recv aeadSecret
}

func (s aeadSecret) Cipher() cipher.AEAD {
	// Never returns an error on input of correct size.
	return utils.OrPanic1(chacha20poly1305.New(s[:]))
}

func (s dhSecret) AeadSecrets(locIsLeast bool) aeadSecrets {
	hkdf := hkdf.New(sha256.New, s[:], nil, secretConnKeyAndChallengeGen)
	aead := aeadSecrets{}
	// hkdf reader never returns an error.
	utils.OrPanic1(io.ReadFull(hkdf, aead.send[:]))
	utils.OrPanic1(io.ReadFull(hkdf, aead.recv[:]))
	if locIsLeast {
		aead.send, aead.recv = aead.recv, aead.send
	}
	return aead
}

// computeDHSecret computes a Diffie-Hellman shared secret key
// from our own local private key and the other's public key.
func (s ephSecret) DhSecret(remPubKey ephPublic) (dhSecret, error) {
	dhSecretRaw, err := curve25519.X25519(s.secret[:], remPubKey[:])
	if err != nil {
		return dhSecret{}, fmt.Errorf("%w: %v", errDH, err)
	}
	return dhSecret(dhSecretRaw), nil
}
