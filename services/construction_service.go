// Copyright 2020 Coinbase, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package services

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/crypto"
	"github.com/leeheonseung/rosetta-icon/icon"
	"github.com/leeheonseung/rosetta-icon/icon/client_v1"
	"strconv"
	"time"

	"github.com/leeheonseung/rosetta-icon/configuration"

	"github.com/coinbase/rosetta-sdk-go/parser"
	"github.com/coinbase/rosetta-sdk-go/types"
)

type ConstructionAPIService struct {
	config *configuration.Configuration
	client Client
}

func NewConstructionAPIService(
	cfg *configuration.Configuration,
	client Client,
) *ConstructionAPIService {
	return &ConstructionAPIService{
		config: cfg,
		client: client,
	}
}

func (s *ConstructionAPIService) ConstructionDerive(
	ctx context.Context,
	request *types.ConstructionDeriveRequest,
) (*types.ConstructionDeriveResponse, *types.Error) {
	pubkey, err := crypto.ParsePublicKey(request.PublicKey.Bytes)
	if err != nil {
		return nil, wrapErr(ErrUnableToDecompressPubkey, err)
	}

	addr := common.NewAccountAddressFromPublicKey(pubkey)
	return &types.ConstructionDeriveResponse{
		AccountIdentifier: &types.AccountIdentifier{
			Address: addr.String(),
		},
	}, nil
}

func (s *ConstructionAPIService) ConstructionPreprocess(
	ctx context.Context,
	request *types.ConstructionPreprocessRequest,
) (*types.ConstructionPreprocessResponse, *types.Error) {
	descriptions := &parser.Descriptions{
		OperationDescriptions: []*parser.OperationDescription{
			{
				Type: client_v1.CallOpType,
				Account: &parser.AccountDescription{
					Exists: true,
				},
				Amount: &parser.AmountDescription{
					Exists:   true,
					Sign:     parser.NegativeAmountSign,
					Currency: client_v1.ICXCurrency,
				},
			},
			{
				Type: client_v1.CallOpType,
				Account: &parser.AccountDescription{
					Exists: true,
				},
				Amount: &parser.AmountDescription{
					Exists:   true,
					Sign:     parser.PositiveAmountSign,
					Currency: client_v1.ICXCurrency,
				},
			},
		},
		ErrUnmatched: true,
	}

	m, err := parser.MatchOperations(descriptions, request.Operations)
	if err != nil {
		return nil, wrapErr(ErrUnclearIntent, err)
	}

	f, _ := m[0].First()
	fa := f.Account.Address
	t, _ := m[1].First()
	ta := t.Account.Address

	// Ensure valid from address
	e := icon.CheckAddress(fa)
	if e != nil {
		return nil, wrapErr(e, fmt.Errorf("%s is not a valid address", fa))
	}

	// Ensure valid to address
	e = icon.CheckAddress(ta)
	if e != nil {
		return nil, wrapErr(e, fmt.Errorf("%s is not a valid address", ta))
	}

	preprocessOutput := &options{
		From: fa,
	}

	marshaled, err := marshalJSONMap(preprocessOutput)
	if err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	return &types.ConstructionPreprocessResponse{
		Options: marshaled,
	}, nil
}

func (s *ConstructionAPIService) ConstructionMetadata(
	ctx context.Context,
	request *types.ConstructionMetadataRequest,
) (*types.ConstructionMetadataResponse, *types.Error) {
	if s.config.Mode != configuration.Online {
		return nil, ErrUnavailableOffline
	}

	var input options
	if err := unmarshalJSONMap(request.Options, &input); err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	metadata := &metadata{
		StepPrice: client_v1.StepPrice,
	}

	metadataMap, err := marshalJSONMap(metadata)
	if err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	fee := metadata.StepPrice.Int64() * client_v1.TransferStepPrice.Int64()

	return &types.ConstructionMetadataResponse{
		Metadata: metadataMap,
		SuggestedFee: []*types.Amount{
			{
				Value:    strconv.FormatInt(fee, 10),
				Currency: client_v1.ICXCurrency,
			},
		},
	}, nil
}

func (s *ConstructionAPIService) ConstructionPayloads(
	ctx context.Context,
	request *types.ConstructionPayloadsRequest,
) (*types.ConstructionPayloadsResponse, *types.Error) {
	d := &parser.Descriptions{
		OperationDescriptions: []*parser.OperationDescription{
			{
				Type: client_v1.CallOpType,
				Account: &parser.AccountDescription{
					Exists: true,
				},
				Amount: &parser.AmountDescription{
					Exists:   true,
					Sign:     parser.NegativeAmountSign,
					Currency: client_v1.ICXCurrency,
				},
			},
			{
				Type: client_v1.CallOpType,
				Account: &parser.AccountDescription{
					Exists: true,
				},
				Amount: &parser.AmountDescription{
					Exists:   true,
					Sign:     parser.PositiveAmountSign,
					Currency: client_v1.ICXCurrency,
				},
			},
		},
		ErrUnmatched: true,
	}
	m, err := parser.MatchOperations(d, request.Operations)
	if err != nil {
		return nil, wrapErr(ErrUnclearIntent, err)
	}

	// Required Fields for constructing a ICON transaction
	tOp, amount := m[1].First()
	ta := tOp.Account.Address
	//nid := s.config.Network.Network

	// Additional Fields for constructing custom ICON tx struct
	fOp, _ := m[0].First()
	fa := fOp.Account.Address
	uTx := &client_v1.TransactionV3{
		Version:   common.HexUint16{Value: 3},
		From:      *common.NewAddressFromString(fa),
		To:        *common.NewAddressFromString(ta),
		Value:     common.NewHexInt(amount.Int64()),
		StepLimit: *common.NewHexInt(client_v1.TransferStepLimit.Int64()),
		Timestamp: common.HexInt64{Value: time.Now().UTC().UnixNano()},
		NID:       &common.HexInt64{Value: 3},
		Nonce:     common.NewHexInt(22),
	}
	h, err := uTx.CalcHash()
	if err != nil {
		return nil, ErrUnclearIntent
	}

	payload := &types.SigningPayload{
		AccountIdentifier: &types.AccountIdentifier{Address: fa},
		Bytes:             h,
		SignatureType:     types.EcdsaRecovery,
	}

	unsignedTxJSON, err := json.Marshal(uTx)
	if err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	return &types.ConstructionPayloadsResponse{
		UnsignedTransaction: string(unsignedTxJSON),
		Payloads:            []*types.SigningPayload{payload},
	}, nil
}

func (s *ConstructionAPIService) ConstructionCombine(
	ctx context.Context,
	request *types.ConstructionCombineRequest,
) (*types.ConstructionCombineResponse, *types.Error) {
	var unsignedTx client_v1.TransactionV3
	var err error
	if err = json.Unmarshal([]byte(request.UnsignedTransaction), &unsignedTx); err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	var sig common.Signature
	sigBytes := request.Signatures[0].Bytes
	sig.Signature, err = crypto.ParseSignature(sigBytes)
	if err != nil {
		return nil, wrapErr(ErrSignatureInvalid, err)
	}

	signedTx := client_v1.TransactionV3WithSig{
		TransactionV3: unsignedTx,
		Signature:     sig,
	}

	if err = signedTx.VerifySignature(); err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	signedTxJSON, err := json.Marshal(signedTx)
	if err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	return &types.ConstructionCombineResponse{
		SignedTransaction: string(signedTxJSON),
	}, nil
}

func (s *ConstructionAPIService) ConstructionHash(
	ctx context.Context,
	request *types.ConstructionHashRequest,
) (*types.TransactionIdentifierResponse, *types.Error) {
	signedTx := &client_v1.TransactionV3WithSig{}
	if err := json.Unmarshal([]byte(request.SignedTransaction), signedTx); err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}
	h := "0x" + hex.EncodeToString(signedTx.TxHash())

	return &types.TransactionIdentifierResponse{
		TransactionIdentifier: &types.TransactionIdentifier{
			Hash: h,
		},
	}, nil
}

// ConstructionParse implements the /construction/parse endpoint.
func (s *ConstructionAPIService) ConstructionParse(
	ctx context.Context,
	request *types.ConstructionParseRequest,
) (*types.ConstructionParseResponse, *types.Error) {
	return nil, nil
}

func (s *ConstructionAPIService) ConstructionSubmit(
	ctx context.Context,
	request *types.ConstructionSubmitRequest,
) (*types.TransactionIdentifierResponse, *types.Error) {
	if s.config.Mode != configuration.Online {
		return nil, ErrUnavailableOffline
	}

	var signedTx client_v1.TransactionV3WithSig
	if _, err := client_v1.ParseV3JSON([]byte(request.SignedTransaction), false); err != nil {
		return nil, wrapErr(ErrUnableToParseIntermediateResult, err)
	}

	//if err := s.client.SendTransaction(ctx, &signedTx); err != nil {
	//	return nil, wrapErr(ErrBroadcastFailed, err)
	//}

	h := "0x" + hex.EncodeToString(signedTx.TxHash())
	txIdentifier := &types.TransactionIdentifier{
		Hash: h,
	}

	return &types.TransactionIdentifierResponse{
		TransactionIdentifier: txIdentifier,
	}, nil
}