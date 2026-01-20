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
	"net"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/oasisprotocol/curve25519-voi/primitives/merlin"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	pb "github.com/tendermint/tendermint/proto/tendermint/p2p"
)

var errDH = errors.New("DH handshake failed")
var errAuth = errors.New("authentication failed")
var errAEAD = errors.New("decoding failed")

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
	conn      net.Conn
	challenge [32]byte
	recvState utils.Mutex[*recvState]
	sendState utils.Mutex[*sendState]
	remPubKey crypto.PubKey
}

func newSecretConnection(conn net.Conn, loc ephSecret, rem ephPublic) (*SecretConnection, error) {
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
	var challenge [32]byte
	transcript.ExtractBytes(challenge[:], labelSecretConnectionMac)

	// Generate the secret used for receiving, sending, challenge via HKDF-SHA2
	// on the transcript state (which itself also uses HKDF-SHA2 to derive a key
	// from the dhSecret).
	aead := dh.AeadSecrets(loc.public == pubs[0])
	return &SecretConnection{
		conn:      conn,
		challenge: challenge,
		recvState: utils.NewMutex(newRecvState(aead.recv.Cipher())),
		sendState: utils.NewMutex(newSendState(aead.send.Cipher())),
	}, nil
}

// MakeSecretConnection performs handshake and returns a new authenticated
// SecretConnection.
// Returns nil if there is an error in handshake.
// Caller should call conn.Close()
// See docs/sts-final.pdf for more information.
func MakeSecretConnection(ctx context.Context, conn net.Conn, locPrivKey crypto.PrivKey) (*SecretConnection, error) {
	// Generate ephemeral key for perfect forward secrecy.
	loc := genEphKey()

	// Write local ephemeral pubkey and receive one too.
	// NOTE: every 32-byte string is accepted as a Curve25519 public key (see
	// DJB's Curve25519 paper: http://cr.yp.to/ecdh/curve25519-20060209.pdf)
	rem, err := shareEphPubKey(ctx, conn, loc.public)
	if err != nil {
		return nil, err
	}
	sc, err := newSecretConnection(conn, loc, rem)
	if err != nil {
		return nil, err
	}
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*SecretConnection, error) {
		// Share (in secret) each other's pubkey & challenge signature
		s.Spawn(func() error {
			loc := &authSigMessage{locPrivKey.Public(), locPrivKey.Sign(sc.challenge[:])}
			_, err := protoio.NewDelimitedWriter(sc).WriteMsg(authSigMessageConv.Encode(loc))
			if err != nil {
				return err
			}
			return sc.Flush()
		})
		var pba pb.AuthSigMessage
		if _, err := protoio.NewDelimitedReader(sc, 1024*1024).ReadMsg(&pba); err != nil {
			return nil, fmt.Errorf("%w: %v", errAuth, err)
		}
		rem, err := authSigMessageConv.Decode(&pba)
		if err != nil {
			return nil, fmt.Errorf("%w: authSigMessageFromProto(): %v", errAuth, err)
		}
		if err := rem.Key.Verify(sc.challenge[:], rem.Sig); err != nil {
			return nil, fmt.Errorf("%w: %v", errAuth, err)
		}
		sc.remPubKey = rem.Key
		return sc, nil
	})
}

// RemotePubKey returns authenticated remote pubkey
func (sc *SecretConnection) RemotePubKey() crypto.PubKey { return sc.remPubKey }

// Writes encrypted frames of `totalFrameSize + aeadSizeOverhead`.
func (sc *SecretConnection) Write(data []byte) (int, error) {
	for sendState := range sc.sendState.Lock() {
		n := 0
		for {
			chunk := min(len(data), cap(sendState.data)-len(sendState.data))
			sendState.data = append(sendState.data, data[:chunk]...)
			n += chunk
			data = data[chunk:]
			if len(data) == 0 {
				return n, nil
			}
			if err := sc.flush(sendState); err != nil {
				return n, err
			}
		}
	}
	panic("unreachable")
}

func (sc *SecretConnection) flush(sendState *sendState) error {
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
	_, err := sc.conn.Write(sendState.cipher.Seal(sealedFrame[:0], nonce[:], sendState.frame[:], nil))
	// Zeroize the the frame to avoid resending data from the previous frame.
	// Security-wise it doesn't make any difference, it is here just to avoid people raising concerns.
	clear(sendState.frame)
	sendState.data = sendState.frame[dataSizeLen:dataSizeLen]
	return err
}

func (sc *SecretConnection) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	for recvState := range sc.recvState.Lock() {
		for len(recvState.data) == 0 {
			var sealedFrame [frameSize + aeadOverhead]byte
			if _, err := io.ReadFull(sc.conn, sealedFrame[:]); err != nil {
				return 0, err
			}
			if recvState.nonce == math.MaxUint64 {
				return 0, fmt.Errorf("nonce overflow")
			}
			var nonce [aeadNonceSize]byte
			binary.LittleEndian.PutUint64(nonce[4:], recvState.nonce)
			recvState.nonce += 1
			if _, err := recvState.cipher.Open(recvState.frame[:0], nonce[:], sealedFrame[:], nil); err != nil {
				return 0, fmt.Errorf("%w: %v", errAEAD, err)
			}
			dataSize := binary.LittleEndian.Uint32(recvState.frame)
			if dataSize > dataSizeMax {
				return 0, errors.New("dataSize is greater than dataSizeMax")
			}
			recvState.data = recvState.frame[dataSizeLen : dataSizeLen+dataSize]
		}
		n := copy(data, recvState.data)
		recvState.data = recvState.data[n:]
		return n, nil
	}
	panic("unreachable")
}

// Implements Conn
func (sc *SecretConnection) Flush() error {
	for sendState := range sc.sendState.Lock() {
		return sc.flush(sendState)
	}
	panic("unreachable")
}

func (sc *SecretConnection) Close() error { return sc.conn.Close() }

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

func shareEphPubKey(ctx context.Context, conn io.ReadWriter, locEphPub ephPublic) (ephPublic, error) {
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (ephPublic, error) {
		s.Spawn(func() error {
			_, err := protoio.NewDelimitedWriter(conn).WriteMsg(&gogotypes.BytesValue{Value: locEphPub[:]})
			return err
		})
		var bytes gogotypes.BytesValue
		if _, err := protoio.NewDelimitedReader(conn, 1024*1024).ReadMsg(&bytes); err != nil {
			return ephPublic{}, fmt.Errorf("%w: %v", errDH, err)
		}
		if len(bytes.Value) != len(ephPublic{}) {
			return ephPublic{}, fmt.Errorf("%w: bad ephemeral key size", errDH)
		}
		return ephPublic(bytes.Value), nil
	})
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

type authSigMessage struct {
	Key crypto.PubKey
	Sig crypto.Sig
}

var authSigMessageConv = utils.ProtoConv[*authSigMessage, *pb.AuthSigMessage]{
	Encode: func(m *authSigMessage) *pb.AuthSigMessage {
		return &pb.AuthSigMessage{
			PubKey: crypto.PubKeyConv.Encode(m.Key),
			Sig:    m.Sig.Bytes(),
		}
	},
	Decode: func(p *pb.AuthSigMessage) (*authSigMessage, error) {
		key, err := crypto.PubKeyConv.DecodeReq(p.PubKey)
		if err != nil {
			return nil, fmt.Errorf("PubKey: %w", err)
		}
		sig, err := crypto.SigFromBytes(p.Sig)
		if err != nil {
			return nil, fmt.Errorf("Sig: %w", err)
		}
		return &authSigMessage{key, sig}, nil
	},
}
