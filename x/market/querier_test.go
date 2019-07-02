package market

import (
	"strings"
	"testing"

	"github.com/terra-project/core/types/assets"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const custom = "custom"

func getQueriedParams(t *testing.T, ctx sdk.Context, cdc *codec.Codec, querier sdk.Querier) Params {
	query := abci.RequestQuery{
		Path: strings.Join([]string{custom, QuerierRoute, QueryParams}, "/"),
		Data: []byte{},
	}

	bz, err := querier(ctx, []string{QueryParams}, query)
	require.Nil(t, err)
	require.NotNil(t, bz)

	var params Params
	err2 := cdc.UnmarshalJSON(bz, &params)
	require.Nil(t, err2)

	return params
}

func getQueriedSwap(t *testing.T, ctx sdk.Context, cdc *codec.Codec, querier sdk.Querier, askDenom string, offerCoin sdk.Coin) sdk.Coin {
	query := abci.RequestQuery{
		Path: strings.Join([]string{custom, QuerierRoute, QuerySwap}, "/"),
		Data: cdc.MustMarshalJSON(NewQuerySwapParams(offerCoin)),
	}

	bz, err := querier(ctx, []string{QuerySwap, askDenom}, query)
	require.Nil(t, err)
	require.NotNil(t, bz)

	var response sdk.Coin
	err2 := cdc.UnmarshalJSON(bz, &response)
	require.Nil(t, err2)
	return response
}

func TestQueryParams(t *testing.T) {
	input := createTestInput(t)
	querier := NewQuerier(input.marketKeeper)

	defaultParams := DefaultParams()
	input.marketKeeper.SetParams(input.ctx, defaultParams)

	params := getQueriedParams(t, input.ctx, input.cdc, querier)

	require.Equal(t, defaultParams, params)
}

func TestQuerySwap(t *testing.T) {
	input := createTestInput(t)
	querier := NewQuerier(input.marketKeeper)

	offerCoin := sdk.NewCoin(assets.MicroLunaDenom, sdk.NewInt(10000))

	// With price registered
	testPrice := sdk.NewDecWithPrec(48842, 4)
	input.oracleKeeper.SetLunaSwapRate(input.ctx, assets.MicroKRWDenom, testPrice)

	price := getQueriedSwap(t, input.ctx, input.cdc, querier, assets.MicroKRWDenom, offerCoin)

	require.NotNil(t, price)
}
