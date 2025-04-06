package attest

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/fxamacker/cbor/v2"
	"github.com/hf/nsm"
	"github.com/hf/nsm/ioc"
	"github.com/hf/nsm/request"
	"github.com/rs/zerolog"
)

type NSMRawDocument struct {
	RawDocument0 []byte `json:"rawDocument0"`
	RawDocument1 []byte `json:"rawDocument1"`
}

func GetNSMAttesation(logger *zerolog.Logger) (*NSMRawDocument, error) {
	// create private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	// call nsm with private key
	rawNsmDocument0, err := getNSMDocument0(privateKey)
	if err != nil {
		logger.Error().Msgf("failed to get NSM document: %w", err)
	} else {
		logger.Info().Str("rawNsmDocument0", fmt.Sprintf("%v", rawNsmDocument0)).Str("rawNsmDocument0_base64", base64.StdEncoding.EncodeToString(rawNsmDocument0)).Msg("NSM document0")
	}
	rawNsmDocument1, err := getNSMDocument1(privateKey)
	if err != nil {
		logger.Error().Msgf("failed to get NSM document: %w", err)
	} else {
		logger.Info().Str("rawNsmDocument1", fmt.Sprintf("%v", rawNsmDocument1)).Str("rawNsmDocument1_base64", base64.StdEncoding.EncodeToString(rawNsmDocument1)).Msg("NSM document1")
	}

	// return the document
	return &NSMRawDocument{
		RawDocument0: rawNsmDocument0,
		RawDocument1: rawNsmDocument1,
	}, nil

}

func getNSMDocument0(privateKey *rsa.PrivateKey) ([]byte, error) {
	// create a new session
	session, err := nsm.OpenDefaultSession()
	if err != nil {
		return nil, fmt.Errorf("failed to open NSM session: %w", err)
	}
	defer session.Close()

	// create a new attestation request
	req := &request.Attestation{
		PublicKey: x509.MarshalPKCS1PublicKey(&privateKey.PublicKey),
	}

	// send the request
	res, err := session.Send(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send attestation request: %w", err)
	}

	// check for errors
	if res.Error != "" {
		return nil, fmt.Errorf("NSM returned error: %s", res.Error)
	}

	// return the document
	return res.Attestation.Document, nil
}

func getNSMDocument1(privateKey *rsa.PrivateKey) ([]byte, error) {
	// create a new session
	nsmFd, err := os.Open("/dev/nsm")
	if err != nil {
		return nil, fmt.Errorf("failed to open NSM device: %w", err)
	}
	defer nsmFd.Close()

	// create a new attestation request
	req := &request.Attestation{
		PublicKey: x509.MarshalPKCS1PublicKey(&privateKey.PublicKey),
	}
	var reqb bytes.Buffer
	encoder := cbor.NewEncoder(&reqb)
	err = encoder.Encode(req.Encoded())
	if nil != err {
		return nil, fmt.Errorf("failed to encode attestation request: %w", err)
	}
	res := make([]byte, 0x3000)
	res, err = send(nsmFd.Fd(), reqb.Bytes(), res)
	if err != nil {
		return nil, fmt.Errorf("failed to send attestation request: %w", err)
	}

	return res, nil
}
func send(fd uintptr, req []byte, res []byte) ([]byte, error) {
	iovecReq := syscall.Iovec{
		Base: &req[0],
	}
	iovecReq.SetLen(len(req))

	iovecRes := syscall.Iovec{
		Base: &res[0],
	}
	iovecRes.SetLen(len(res))

	type ioctlMessage struct {
		Request  syscall.Iovec
		Response syscall.Iovec
	}

	msg := ioctlMessage{
		Request:  iovecReq,
		Response: iovecRes,
	}
	const ioctlMagic = 0x0A

	_, _, err := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(ioc.Command(ioc.READ|ioc.WRITE, ioctlMagic, 0, uint(unsafe.Sizeof(msg)))),
		uintptr(unsafe.Pointer(&msg)),
	)

	if 0 != err {
		return nil, fmt.Errorf("failed to send attestation request: %w", err)
	}

	return res[:msg.Response.Len], nil
}
