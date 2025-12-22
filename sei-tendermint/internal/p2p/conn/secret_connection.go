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
	"time"

	gogotypes "github.com/gogo/protobuf/types"
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/oasisprotocol/curve25519-voi/primitives/merlin"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/encoding"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	tmp2p "github.com/tendermint/tendermint/proto/tendermint/p2p"
)

// 4 + 1024 == 1028 total frame size
const (
	dataLenSize      = 4
	dataMaxSize      = 1024
	totalFrameSize   = dataMaxSize + dataLenSize
	aeadSizeOverhead = 16 // overhead of poly 1305 authentication tag
	aeadNonceSize    = chacha20poly1305.NonceSize

	labelEphemeralLowerPublicKey = "EPHEMERAL_LOWER_PUBLIC_KEY"
	labelEphemeralUpperPublicKey = "EPHEMERAL_UPPER_PUBLIC_KEY"
	labelDHSecret                = "DH_SECRET"
	labelSecretConnectionMac     = "SECRET_CONNECTION_MAC"
)

var ErrSmallOrderRemotePubKey = errors.New("detected low order point from remote peer")
var secretConnKeyAndChallengeGen = []byte("TENDERMINT_SECRET_CONNECTION_KEY_AND_CHALLENGE_GEN")

type nonce [aeadNonceSize]byte

// Increment nonce little-endian by 1 with wraparound.
// Due to chacha20poly1305 expecting a 12 byte nonce we do not use the first four
// bytes. We only increment a 64 bit unsigned int in the remaining 8 bytes
// (little-endian in nonce[4:]).
func (n *nonce) inc() {
	counter := binary.LittleEndian.Uint64(n[4:])
	if counter == math.MaxUint64 {
		// Terminates the session and makes sure the nonce would not re-used.
		// See https://github.com/tendermint/tendermint/issues/3531
		panic("can't increase nonce without overflow")
	}
	counter++
	binary.LittleEndian.PutUint64(n[4:], counter)
}

type sendState struct {
	cipher cipher.AEAD
	nonce  nonce
}

type recvState struct {
	cipher cipher.AEAD
	buffer []byte
	nonce  nonce
}

// SecretConnection implements net.Conn.
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

func newSecretConnection(conn net.Conn, loc ephSecret, rem ephPublic) *SecretConnection {
	pubs := utils.Slice(loc.public, rem)
	if bytes.Compare(pubs[0][:], pubs[1][:]) > 0 {
		pubs[0], pubs[1] = pubs[1], pubs[0]
	}
	transcript := merlin.NewTranscript("TENDERMINT_SECRET_CONNECTION_TRANSCRIPT_HASH")
	transcript.AppendMessage(labelEphemeralLowerPublicKey, pubs[0][:])
	transcript.AppendMessage(labelEphemeralUpperPublicKey, pubs[1][:])
	dh := loc.DhSecret(rem)
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
		recvState: utils.NewMutex(&recvState{cipher: aead.recv.Cipher()}),
		sendState: utils.NewMutex(&sendState{cipher: aead.send.Cipher()}),
	}
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
	sc := newSecretConnection(conn, loc, rem)

	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*SecretConnection, error) {
		// Share (in secret) each other's pubkey & challenge signature
		s.Spawn(func() error {
			loc := authSigMessage{locPrivKey.Public(), locPrivKey.Sign(sc.challenge[:])}
			_, err := protoio.NewDelimitedWriter(sc).WriteMsg(loc.ToProto())
			return err
		})
		var pba tmp2p.AuthSigMessage
		if _, err := protoio.NewDelimitedReader(sc, 1024*1024).ReadMsg(&pba); err != nil {
			return nil, err
		}
		rem, err := authSigMessageFromProto(&pba)
		if err != nil {
			return nil, fmt.Errorf("authSigMessageFromProto(): %w", err)
		}
		if err := rem.Key.Verify(sc.challenge[:], rem.Sig); err != nil {
			return nil, fmt.Errorf("challenge verification failed: %w", err)
		}
		sc.remPubKey = rem.Key
		return sc, nil
	})
}

// RemotePubKey returns authenticated remote pubkey
func (sc *SecretConnection) RemotePubKey() crypto.PubKey { return sc.remPubKey }

// Writes encrypted frames of `totalFrameSize + aeadSizeOverhead`.
// CONTRACT: data smaller than dataMaxSize is written atomically.
func (sc *SecretConnection) Write(data []byte) (n int, err error) {
	for sendState := range sc.sendState.Lock() {
		for 0 < len(data) {
			if err := func() error {
				var sealedFrame = pool.Get(aeadSizeOverhead + totalFrameSize)
				var frame = pool.Get(totalFrameSize)
				defer func() {
					pool.Put(sealedFrame)
					pool.Put(frame)
				}()
				var chunk []byte
				if dataMaxSize < len(data) {
					chunk = data[:dataMaxSize]
					data = data[dataMaxSize:]
				} else {
					chunk = data
					data = nil
				}
				chunkLength := len(chunk)
				binary.LittleEndian.PutUint32(frame, uint32(chunkLength))
				copy(frame[dataLenSize:], chunk)

				// encrypt the frame
				sendState.cipher.Seal(sealedFrame[:0], sendState.nonce[:], frame, nil)
				sendState.nonce.inc()
				// end encryption

				_, err = sc.conn.Write(sealedFrame)
				if err != nil {
					return err
				}
				n += len(chunk)
				return nil
			}(); err != nil {
				return n, err
			}
		}
		return n, err
	}
	panic("unreachable")
}

var errAEAD = errors.New("decoding failed")

// CONTRACT: data smaller than dataMaxSize is read atomically.
func (sc *SecretConnection) Read(data []byte) (n int, err error) {
	for recvState := range sc.recvState.Lock() {
		// read off and update the recvBuffer, if non-empty
		if 0 < len(recvState.buffer) {
			n = copy(data, recvState.buffer)
			recvState.buffer = recvState.buffer[n:]
			return
		}

		// read off the conn
		var sealedFrame = pool.Get(aeadSizeOverhead + totalFrameSize)
		defer pool.Put(sealedFrame)
		_, err = io.ReadFull(sc.conn, sealedFrame)
		if err != nil {
			return
		}

		// decrypt the frame.
		// reads and updates the sc.recvNonce
		var frame = pool.Get(totalFrameSize)
		defer pool.Put(frame)
		_, err = recvState.cipher.Open(frame[:0], recvState.nonce[:], sealedFrame, nil)
		if err != nil {
			return n, fmt.Errorf("%w: %v", errAEAD, err)
		}
		recvState.nonce.inc()
		// end decryption

		// copy checkLength worth into data,
		// set recvBuffer to the rest.
		var chunkLength = binary.LittleEndian.Uint32(frame) // read the first four bytes
		if chunkLength > dataMaxSize {
			return 0, errors.New("chunkLength is greater than dataMaxSize")
		}
		var chunk = frame[dataLenSize : dataLenSize+chunkLength]
		n = copy(data, chunk)
		if n < len(chunk) {
			recvState.buffer = make([]byte, len(chunk)-n)
			copy(recvState.buffer, chunk[n:])
		}
		return n, err
	}
	panic("unreachable")
}

// Implements net.Conn
func (sc *SecretConnection) Close() error                       { return sc.conn.Close() }
func (sc *SecretConnection) LocalAddr() net.Addr                { return sc.conn.LocalAddr() }
func (sc *SecretConnection) RemoteAddr() net.Addr               { return sc.conn.RemoteAddr() }
func (sc *SecretConnection) SetDeadline(t time.Time) error      { return sc.conn.SetDeadline(t) }
func (sc *SecretConnection) SetReadDeadline(t time.Time) error  { return sc.conn.SetReadDeadline(t) }
func (sc *SecretConnection) SetWriteDeadline(t time.Time) error { return sc.conn.SetWriteDeadline(t) }

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

var errDH = errors.New("DH handshake failed")

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
func (s ephSecret) DhSecret(remPubKey ephPublic) dhSecret {
	return dhSecret(utils.OrPanic1(curve25519.X25519(s.secret[:], remPubKey[:])))
}

type authSigMessage struct {
	Key crypto.PubKey
	Sig crypto.Sig
}

func (m *authSigMessage) ToProto() *tmp2p.AuthSigMessage {
	return &tmp2p.AuthSigMessage{
		PubKey: encoding.PubKeyToProto(m.Key),
		Sig:    m.Sig.Bytes(),
	}
}

func authSigMessageFromProto(p *tmp2p.AuthSigMessage) (*authSigMessage, error) {
	key, err := encoding.PubKeyFromProto(p.PubKey)
	if err != nil {
		return nil, fmt.Errorf("PubKey: %w", err)
	}
	sig, err := crypto.SigFromBytes(p.Sig)
	if err != nil {
		return nil, fmt.Errorf("Sig: %w", err)
	}
	return &authSigMessage{key, sig}, nil
}
