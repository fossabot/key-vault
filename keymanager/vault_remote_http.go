package keymanager

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/sirupsen/logrus"

	"github.com/bloxapp/key-vault/backend"
	"github.com/bloxapp/key-vault/utils/endpoint"
	"github.com/bloxapp/key-vault/utils/httpex"
)

// Signing endpoints
var (
	signAggregationPath = endpoint.Build(backend.SignAggregationPattern)
	signProposalPath    = endpoint.Build(backend.SignProposalPattern)
	signAttestationPath = endpoint.Build(backend.SignAttestationPattern)
)

// Predefined errors
var (
	ErrUnprotectedSigning = NewGenericErrorWithMessage("remote HTTP key manager does not support unprotected signing")
	ErrNoSuchKey          = NewGenericErrorWithMessage("no such key")
)

// VaultRemoteHTTPWallet is a key manager that accesses a remote vault wallet daemon through HTTP connection.
type VaultRemoteHTTPWallet struct {
	remoteAddress string
	accessToken   string
	originPubKey  string
	pubKey        [48]byte
	httpClient    *http.Client

	log *logrus.Entry
}

// NewVaultRemoteHTTPWalletFromOpts is the constructor of VaultRemoteHTTPWallet.
// This constructor handles the given options and creates a wallet.
func NewVaultRemoteHTTPWalletFromOpts(input string) (*VaultRemoteHTTPWallet, string, error) {
	opts := &remoteOpts{}
	if err := json.Unmarshal([]byte(input), opts); err != nil {
		return nil, remoteOptsHelp, NewGenericError(err, "failed to unmarshal options")
	}

	if len(opts.Location) == 0 {
		return nil, remoteOptsHelp, NewGenericErrorMessage("wallet location is required")
	}
	if len(opts.AccessToken) == 0 {
		return nil, remoteOptsHelp, NewGenericErrorMessage("wallet access token is required")
	}
	if len(opts.PubKey) == 0 {
		return nil, remoteOptsHelp, NewGenericErrorMessage("wallet public key is required")
	}

	logger := logrus.New().WithFields(logrus.Fields{
		"location":   opts.Location,
		"public_key": opts.PubKey,
	})

	decodedPubKey, err := hex.DecodeString(opts.PubKey)
	if err != nil {
		return nil, remoteOptsHelp, NewGenericError(err, "failed to hex decode public key '%s'", opts.PubKey)
	}

	return &VaultRemoteHTTPWallet{
		remoteAddress: opts.Location,
		accessToken:   opts.AccessToken,
		originPubKey:  opts.PubKey,
		pubKey:        bytesutil.ToBytes48(decodedPubKey),
		httpClient:    httpex.CreateClient(),
		log:           logger,
	}, remoteOptsHelp, nil
}

// NewVaultRemoteHTTPWallet is the constructor of VaultRemoteHTTPWallet.
func NewVaultRemoteHTTPWallet(log *logrus.Entry, remoteAddress, accessToken, pubKey string) (*VaultRemoteHTTPWallet, error) {
	// Decode public key
	decodedPubKey, err := hex.DecodeString(pubKey)
	if err != nil {
		return nil, NewGenericError(err, "failed to hex decode public key '%s'", pubKey)
	}

	return &VaultRemoteHTTPWallet{
		remoteAddress: remoteAddress,
		accessToken:   accessToken,
		originPubKey:  pubKey,
		pubKey:        bytesutil.ToBytes48(decodedPubKey),
		httpClient:    httpex.CreateClient(),
		log:           log,
	}, nil
}

// Sign implements KeyManager interface.
func (km *VaultRemoteHTTPWallet) Sign(pubKey [48]byte, root [32]byte) (bls.Signature, error) {
	return nil, ErrUnprotectedSigning
}

// SignGeneric implements ProtectingKeyManager interface.
func (km *VaultRemoteHTTPWallet) SignGeneric(pubKey [48]byte, root [32]byte, domain [32]byte) (bls.Signature, error) {
	if pubKey != km.pubKey {
		return nil, ErrNoSuchKey
	}

	// Prepare request body.
	req := VaultSignAggregationRequest{
		PubKey:     km.originPubKey,
		Domain:     hex.EncodeToString(domain[:]),
		DataToSign: hex.EncodeToString(root[:]),
	}

	// Json encode the request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, NewGenericError(err, "failed to marshal request body")
	}

	// Send request.
	var resp VaultSignResponse
	if err := km.sendRequest(http.MethodPost, signAggregationPath, reqBody, &resp); err != nil {
		km.log.WithError(err).Error("failed to send sign aggregation request")
		return nil, NewGenericError(err, "failed to send SignGeneric request to remote vault wallet")
	}

	// Signature is base64 encoded, so we have to decode that.
	decodedSignature, err := hex.DecodeString(resp.Data.Signature)
	if err != nil {
		return nil, NewGenericError(err, "failed to base64 decode")
	}

	// Get signature from bytes
	sig, err := bls.SignatureFromBytes(decodedSignature)
	if err != nil {
		return nil, NewGenericError(err, "failed to get BLS signature from bytes")
	}

	return sig, nil
}

// SignProposal implements ProtectingKeyManager interface.
func (km *VaultRemoteHTTPWallet) SignProposal(pubKey [48]byte, domain [32]byte, data *ethpb.BeaconBlockHeader) (bls.Signature, error) {
	if pubKey != km.pubKey {
		return nil, ErrNoSuchKey
	}

	// Prepare request body.
	req := VaultSignProposalRequest{
		PubKey:        km.originPubKey,
		Domain:        hex.EncodeToString(domain[:]),
		Slot:          data.GetSlot(),
		ProposerIndex: data.GetProposerIndex(),
		ParentRoot:    hex.EncodeToString(data.GetParentRoot()),
		StateRoot:     hex.EncodeToString(data.GetStateRoot()),
		BodyRoot:      hex.EncodeToString(data.GetBodyRoot()),
	}

	// Json encode the request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, NewGenericError(err, "failed to marshal request body")
	}

	// Send request.
	var resp VaultSignResponse
	if err := km.sendRequest(http.MethodPost, signProposalPath, reqBody, &resp); err != nil {
		km.log.WithError(err).Error("failed to send sign proposal request")
		return nil, NewGenericError(err, "failed to send SignAttestation request to remote vault wallet")
	}

	// Signature is base64 encoded, so we have to decode that.
	decodedSignature, err := hex.DecodeString(resp.Data.Signature)
	if err != nil {
		return nil, NewGenericError(err, "failed to base64 decode")
	}

	// Get signature from bytes
	sig, err := bls.SignatureFromBytes(decodedSignature)
	if err != nil {
		return nil, NewGenericError(err, "failed to get BLS signature from bytes")
	}

	return sig, nil
}

// SignAttestation implements ProtectingKeyManager interface.
func (km *VaultRemoteHTTPWallet) SignAttestation(pubKey [48]byte, domain [32]byte, data *ethpb.AttestationData) (bls.Signature, error) {
	if pubKey != km.pubKey {
		return nil, ErrNoSuchKey
	}

	// Prepare request body.
	req := VaultSignAttestationRequest{
		PubKey:          km.originPubKey,
		Domain:          hex.EncodeToString(domain[:]),
		Slot:            data.GetSlot(),
		CommitteeIndex:  data.GetCommitteeIndex(),
		BeaconBlockRoot: hex.EncodeToString(data.GetBeaconBlockRoot()),
		SourceEpoch:     data.GetSource().GetEpoch(),
		SourceRoot:      hex.EncodeToString(data.GetSource().GetRoot()),
		TargetEpoch:     data.GetTarget().GetEpoch(),
		TargetRoot:      hex.EncodeToString(data.GetTarget().GetRoot()),
	}

	// Json encode the request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, NewGenericError(err, "failed to marshal request body")
	}

	// Send request.
	var resp VaultSignResponse
	if err := km.sendRequest(http.MethodPost, signAttestationPath, reqBody, &resp); err != nil {
		km.log.WithError(err).Error("failed to send sign attestation request")
		return nil, NewGenericError(err, "failed to send SignAttestation request to remote vault wallet")
	}

	// Signature is base64 encoded, so we have to decode that.
	decodedSignature, err := hex.DecodeString(resp.Data.Signature)
	if err != nil {
		return nil, NewGenericError(err, "failed to base64 decode")
	}

	// Get signature from bytes
	sig, err := bls.SignatureFromBytes(decodedSignature)
	if err != nil {
		return nil, NewGenericError(err, "failed to get BLS signature from bytes")
	}

	return sig, nil
}

// FetchValidatingKeys implements KeyManager interface.
func (km *VaultRemoteHTTPWallet) FetchValidatingKeys() ([][48]byte, error) {
	return [][48]byte{km.pubKey}, nil
}

// sendRequest implements the logic to work with HTTP requests.
func (km *VaultRemoteHTTPWallet) sendRequest(method, path string, reqBody []byte, respBody interface{}) error {
	endpoint := km.remoteAddress + path

	// Prepare a new request
	req, err := http.NewRequest(method, endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return NewGenericError(err, "failed to create HTTP request")
	}

	// Pass auth token.
	req.Header.Set("Authorization", "Bearer "+km.accessToken)
	req.Header.Set("Content-Type", "application/json")

	// Send request.
	resp, err := km.httpClient.Do(req)
	if err != nil {
		return NewGenericError(err, "failed to send HTTP request")
	}
	defer resp.Body.Close()

	// Check status code. Must be 200.
	if resp.StatusCode != http.StatusOK {
		responseBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			km.log.WithError(err).Error("failed to read error response body")
		}

		return NewHTTPRequestError(endpoint, resp.StatusCode, responseBody, "unexpected status code")
	}

	// Retrieve response body
	responseBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return NewGenericError(err, "failed to read response body")
	}

	// Read response body into the given object.
	if err := json.Unmarshal(responseBody, &respBody); err != nil {
		return NewGenericError(err, "failed to decode response body")
	}

	return nil
}
