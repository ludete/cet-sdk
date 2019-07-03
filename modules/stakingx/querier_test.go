package stakingx

import (
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

func TestNewQuerier(t *testing.T) {
	//intialize
	sxk, ctx, _ := setUpInput()
	cdc := codec.New()

	pool := staking.Pool{
		BondedTokens:    sdk.NewInt(10e8),
		NotBondedTokens: sdk.NewInt(500e8),
	}
	sxk.sk.SetPool(ctx, pool)
	sxk.SetParams(ctx, DefaultParams())

	//query succeed
	querier := NewQuerier(sxk, cdc)
	path := QueryPool

	_, err := querier(ctx, []string{path}, abci.RequestQuery{})
	require.Nil(t, err)

	//query fail
	failPath := "fake"
	_, err = querier(ctx, []string{failPath}, abci.RequestQuery{})
	require.Equal(t, sdk.CodeUnknownRequest, err.Code())

}