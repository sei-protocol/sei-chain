package conn

import (
	"bytes"
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
	"cmp"
	"context"

	gogotypes "github.com/gogo/protobuf/types"
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/oasisprotocol/curve25519-voi/primitives/merlin"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/encoding"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/crypto"
	tmp2p "github.com/tendermint/tendermint/proto/tendermint/p2p"
)

// 4 + 1024 == 1028 total frame size
const (
	dataLenSize      = 4
	dataMaxSize      = 1024
	totalFrameSize   = dataMaxSize + dataLenSize
	aeadSizeOverhead = 16 // overhead of poly 1305 authentication tag
	aeadKeySize      = chacha20poly1305.KeySize
	aeadNonceSize    = chacha20poly1305.NonceSize

	labelEphemeralLowerPublicKey = "EPHEMERAL_LOWER_PUBLIC_KEY"
	labelEphemeralUpperPublicKey = "EPHEMERAL_UPPER_PUBLIC_KEY"
	labelDHSecret                = "DH_SECRET"
	labelSecretConnectionMac     = "SECRET_CONNECTION_MAC"
)

var (
	ErrSmallOrderRemotePubKey = errors.New("detected low order point from remote peer")

	secretConnKeyAndChallengeGen = []byte("TENDERMINT_SECRET_CONNECTION_KEY_AND_CHALLENGE_GEN")
)

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

type recvState struct {
	buffer []byte
	nonce nonce
}

type Challenge [32]byte

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
	challenge Challenge
	recvAead cipher.AEAD
	sendAead cipher.AEAD
	remPubKey crypto.PubKey
	conn      net.Conn 
	recvState utils.Mutex[*recvState]
	sendNonce utils.Mutex[*nonce]
}

// MakeSecretConnection performs handshake and returns a new authenticated
// SecretConnection.
// Returns nil if there is an error in handshake.
// Caller should call conn.Close()
// See docs/sts-final.pdf for more information.
func MakeSecretConnection(ctx context.Context, conn net.Conn, locPrivKey crypto.PrivKey) (*SecretConnection, error) {
	// Generate ephemeral keys for perfect forward secrecy.
	locEphPub, locEphPriv := genEphKeys()

	// Write local ephemeral pubkey and receive one too.
	// NOTE: every 32-byte string is accepted as a Curve25519 public key (see
	// DJB's Curve25519 paper: http://cr.yp.to/ecdh/curve25519-20060209.pdf)
	remEphPub, err := shareEphPubKey(ctx, conn, locEphPub)
	if err != nil {
		return nil, err
	}

	// Sort by lexical order.
	pubs := utils.Slice(locEphPub,remEphPub)
	if bytes.Compare(pubs[0][:],pubs[1][:]) > 0 {
		pubs[0],pubs[1] = pubs[1],pubs[0]
	}

	transcript := merlin.NewTranscript("TENDERMINT_SECRET_CONNECTION_TRANSCRIPT_HASH")

	transcript.AppendMessage(labelEphemeralLowerPublicKey, pubs[0][:])
	transcript.AppendMessage(labelEphemeralUpperPublicKey, pubs[1][:])

	// Compute common diffie hellman secret using X25519.
	dhSecret, err := computeDHSecret(remEphPub, locEphPriv)
	if err != nil {
		return nil, err
	}
	transcript.AppendMessage(labelDHSecret, dhSecret[:])

	// Generate the secret used for receiving, sending, challenge via HKDF-SHA2
	// on the transcript state (which itself also uses HKDF-SHA2 to derive a key
	// from the dhSecret).
	recvSecret, sendSecret := deriveSecrets(dhSecret, locEphPub==pubs[0])

	var challenge Challenge 
	transcript.ExtractBytes(challenge[:], labelSecretConnectionMac)

	sendAead, err := chacha20poly1305.New(sendSecret[:])
	if err != nil {
		return nil, errors.New("invalid send SecretConnection Key")
	}
	recvAead, err := chacha20poly1305.New(recvSecret[:])
	if err != nil {
		return nil, errors.New("invalid receive SecretConnection Key")
	}

	sc := &SecretConnection{
		recvAead:   recvAead,
		sendAead:   sendAead,	
		challenge:  challenge,
		conn:       conn,
		recvState:  utils.NewMutex(&recvState{}),
		sendNonce:  utils.NewMutex(&nonce{}),
	}

	// Share (in secret) each other's pubkey & challenge signature
	authSigMsg, err := sc.shareAuthSignature(ctx,locPrivKey)
	if err != nil {
		return nil, err
	}

	remPubKey, remSignature := authSigMsg.Key, authSigMsg.Sig

	if err := remPubKey.Verify(challenge[:], remSignature); err != nil {
		return nil, fmt.Errorf("challenge verification failed: %w", err)
	}

	// We've authorized.
	sc.remPubKey = remPubKey
	return sc, nil
}

// RemotePubKey returns authenticated remote pubkey
func (sc *SecretConnection) RemotePubKey() crypto.PubKey {
	return sc.remPubKey
}

// Writes encrypted frames of `totalFrameSize + aeadSizeOverhead`.
// CONTRACT: data smaller than dataMaxSize is written atomically.
func (sc *SecretConnection) Write(data []byte) (n int, err error) {
	for nonce := range sc.sendNonce.Lock() {
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
				sc.sendAead.Seal(sealedFrame[:0], nonce[:], frame, nil)
				nonce.inc()
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
		_, err = sc.recvAead.Open(frame[:0], recvState.nonce[:], sealedFrame, nil)
		if err != nil {
			return n, fmt.Errorf("failed to decrypt SecretConnection: %w", err)
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
func (sc *SecretConnection) Close() error                  { return sc.conn.Close() }
func (sc *SecretConnection) LocalAddr() net.Addr           { return sc.conn.LocalAddr() }
func (sc *SecretConnection) RemoteAddr() net.Addr          { return sc.conn.RemoteAddr() }
func (sc *SecretConnection) SetDeadline(t time.Time) error { return sc.conn.SetDeadline(t) }
func (sc *SecretConnection) SetReadDeadline(t time.Time) error { return sc.conn.SetReadDeadline(t) }
func (sc *SecretConnection) SetWriteDeadline(t time.Time) error { return sc.conn.SetWriteDeadline(t) }

type ephPublic [32]byte
type ephPrivate *[32]byte

func genEphKeys() (ephPublic, ephPrivate) {
	// TODO: Probably not a problem but ask Tony: different from the rust implementation (uses x25519-dalek),
	// we do not "clamp" the private key scalar:
	// see: https://github.com/dalek-cryptography/x25519-dalek/blob/34676d336049df2bba763cc076a75e47ae1f170f/src/x25519.rs#L56-L74
	public, secret, err := box.GenerateKey(crand.Reader)
	if err!=nil {
		panic(fmt.Errorf("Could not generate ephemeral key-pair: %w",err))
	}
	return *public, secret
}

func shareEphPubKey(ctx context.Context, conn io.ReadWriter, locEphPub ephPublic) (ephPublic, error) {
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (ephPublic,error) {
		s.Spawn(func() error {
			_, err := protoio.NewDelimitedWriter(conn).WriteMsg(&gogotypes.BytesValue{Value: locEphPub[:]})
			return err
		})
		var bytes gogotypes.BytesValue
		if _, err := protoio.NewDelimitedReader(conn, 1024*1024).ReadMsg(&bytes); err!=nil {
			return ephPublic{},err
		}
		if len(bytes.Value)!=len(ephPublic{}) {
			return ephPublic{},errors.New("bad ephemeral key size")
		}
		return ephPublic(bytes.Value),nil
	})
}

func deriveSecrets(
	dhSecret [32]byte,
	locIsLeast bool,
) (recvSecret, sendSecret *[aeadKeySize]byte) {
	hash := sha256.New
	hkdf := hkdf.New(hash, dhSecret[:], nil, secretConnKeyAndChallengeGen)
	// get enough data for 2 aead keys, and a 32 byte challenge
	res := new([2*aeadKeySize + 32]byte)
	_, err := io.ReadFull(hkdf, res[:])
	if err != nil {
		panic(err)
	}

	recvSecret = new([aeadKeySize]byte)
	sendSecret = new([aeadKeySize]byte)

	// bytes 0 through aeadKeySize - 1 are one aead key.
	// bytes aeadKeySize through 2*aeadKeySize -1 are another aead key.
	// which key corresponds to sending and receiving key depends on whether
	// the local key is less than the remote key.
	if locIsLeast {
		copy(recvSecret[:], res[0:aeadKeySize])
		copy(sendSecret[:], res[aeadKeySize:aeadKeySize*2])
	} else {
		copy(sendSecret[:], res[0:aeadKeySize])
		copy(recvSecret[:], res[aeadKeySize:aeadKeySize*2])
	}

	return
}

// computeDHSecret computes a Diffie-Hellman shared secret key
// from our own local private key and the other's public key.
func computeDHSecret(remPubKey ephPublic, locPrivKey ephPrivate) ([32]byte, error) {
	dhs, err := curve25519.X25519(locPrivKey[:], remPubKey[:])
	if err != nil {
		return [32]byte{}, err
	}
	return [32]byte(dhs), nil
}

type authSigMessage struct {
	Key crypto.PubKey
	Sig crypto.Sig
}

func (m *authSigMessage) ToProto() *tmp2p.AuthSigMessage {
	return &tmp2p.AuthSigMessage {
		PubKey: encoding.PubKeyToProto(m.Key),
		Sig: m.Sig.Bytes(),
	}
}

func authSigMessageFromProto(p *tmp2p.AuthSigMessage) (*authSigMessage,error) {
	key, err := encoding.PubKeyFromProto(p.PubKey)
	if err != nil {
		return nil,fmt.Errorf("PubKey: %w", err)
	}
	sig, err := crypto.SigFromBytes(p.Sig)
	if err != nil {
		return nil,fmt.Errorf("Sig: %w", err)
	}
	return &authSigMessage{key,sig},nil
}

func (sc *SecretConnection) shareAuthSignature(ctx context.Context, privKey crypto.PrivKey) (*authSigMessage, error) {
	// Send our info and receive theirs in tandem.
	return scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*authSigMessage,error) {
		s.Spawn(func() error {
			sig := privKey.Sign(sc.challenge[:])
			pk := tmproto.PublicKey{Sum: &tmproto.PublicKey_Ed25519{Ed25519: privKey.Public().Bytes()}}
			msg := &tmp2p.AuthSigMessage{PubKey: pk, Sig: sig.Bytes()}
			_,err := protoio.NewDelimitedWriter(sc).WriteMsg(msg)
			return err
		})
		var pba tmp2p.AuthSigMessage
		if _, err := protoio.NewDelimitedReader(sc, 1024*1024).ReadMsg(&pba); err != nil {
			return nil,err 
		}
		return authSigMessageFromProto(&pba) 
	})
}
