package app

import (
	"sort"
	"testing"

	"github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/gov"

	"github.com/cosmos/cosmos-sdk/x/staking"

	"github.com/coinexchain/dex/modules/authx/types"

	"github.com/stretchr/testify/require"

	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"

	"github.com/coinexchain/dex/testutil"
	dex "github.com/coinexchain/dex/types"
)

func initAppWithValidators(gs GenesisState) *CetChainApp {
	app := newApp()

	// genesis state
	validators := gs.StakingData.Validators
	var validatorUpdates []abci.ValidatorUpdate

	for _, val := range validators {
		validatorUpdates = append(validatorUpdates, val.ABCIValidatorUpdate())
	}

	// init chain
	genStateBytes, _ := app.cdc.MarshalJSON(gs)
	app.InitChain(abci.RequestInitChain{ChainId: testChainID, AppStateBytes: genStateBytes, Validators: validatorUpdates})

	return app
}

func TestExportRestore(t *testing.T) {
	_, _, addr := testutil.KeyPubAddr()
	acc := auth.BaseAccount{Address: addr, Coins: dex.NewCetCoins(1000)}

	// export
	app1 := initAppWithBaseAccounts(acc)
	ctx1 := app1.NewContext(false, abci.Header{Height: app1.LastBlockHeight()})
	genState1 := app1.ExportGenesisState(ctx1)

	// restore & reexport
	app2 := initApp(func(genState *GenesisState) {
		*genState = genState1
	})
	ctx2 := app2.NewContext(false, abci.Header{Height: app2.LastBlockHeight()})
	genState2 := app2.ExportGenesisState(ctx2)

	// check
	json1, err1 := codec.MarshalJSONIndent(app1.cdc, genState1)
	json2, err2 := codec.MarshalJSONIndent(app2.cdc, genState2)
	require.Nil(t, err1)
	require.Nil(t, err2)
	require.Equal(t, json1, json2)
}

func TestExportGenesisState(t *testing.T) {
	_, _, addr := testutil.KeyPubAddr()
	amount := cetToken().GetTotalSupply()
	acc := auth.BaseAccount{Address: addr, Coins: dex.NewCetCoins(amount)}

	// app
	app := initAppWithBaseAccounts(acc)
	ctx := app.NewContext(false, abci.Header{Height: app.LastBlockHeight()})

	accx := types.AccountX{
		Address:      addr,
		MemoRequired: true,
		LockedCoins: []types.LockedCoin{
			{Coin: dex.NewCetCoin(10), UnlockTime: 10},
		},
		FrozenCoins: dex.NewCetCoins(1000),
	}
	app.accountXKeeper.SetAccountX(ctx, accx)

	state := app.ExportGenesisState(ctx)
	sort.Slice(state.Accounts, func(i, j int) bool {
		return state.Accounts[i].ModuleName < state.Accounts[j].ModuleName
	})

	require.Equal(t, 5, len(state.Accounts))
	require.Equal(t, "", state.Accounts[0].ModuleName)
	require.Equal(t, staking.BondedPoolName, state.Accounts[1].ModuleName)
	require.Equal(t, staking.NotBondedPoolName, state.Accounts[2].ModuleName)
	require.Equal(t, distribution.ModuleName, state.Accounts[3].ModuleName)
	require.Equal(t, gov.ModuleName, state.Accounts[4].ModuleName)

	require.Equal(t, sdk.NewInt(amount), state.Accounts[0].Coins.AmountOf("cet"))

	accountX := state.AuthXData.AccountXs[0]
	require.Equal(t, true, accountX.MemoRequired)
	require.Equal(t, int64(10), accountX.LockedCoins[0].UnlockTime)
	require.Equal(t, sdk.NewInt(int64(10)), accountX.LockedCoins[0].Coin.Amount)
	require.Equal(t, "cet", accountX.LockedCoins[0].Coin.Denom)
	require.Equal(t, "1000cet", accountX.FrozenCoins.String())
	require.True(t, state.StakingXData.Params.MinSelfDelegation.IsPositive())
}

func TestExportDefaultAccountXState(t *testing.T) {
	_, _, addr := testutil.KeyPubAddr()
	amount := cetToken().GetTotalSupply()

	acc := auth.BaseAccount{Address: addr, Coins: dex.NewCetCoins(amount)}

	// app
	app := initAppWithBaseAccounts(acc)
	ctx := app.NewContext(false, abci.Header{Height: app.LastBlockHeight()})

	state := app.ExportGenesisState(ctx)
	sort.Slice(state.Accounts, func(i, j int) bool {
		return state.Accounts[i].ModuleName < state.Accounts[j].ModuleName
	})

	require.Equal(t, 5, len(state.Accounts))
	require.Equal(t, sdk.NewInt(amount), state.Accounts[0].Coins.AmountOf("cet"))

	require.Equal(t, 0, len(state.AuthXData.AccountXs))
}

func TestExportAppStateAndValidators(t *testing.T) {
	sk, pk, addr := testutil.KeyPubAddr()
	amount := cetToken().GetTotalSupply()

	acc := auth.BaseAccount{Address: addr, Coins: dex.NewCetCoins(amount)}

	// init app
	app := initApp(func(genState *GenesisState) {
		addGenesisAccounts(genState, acc)
		genState.StakingXData.Params.MinSelfDelegation = sdk.NewInt(1e8)
	})

	app.BeginBlock(abci.RequestBeginBlock{Header: abci.Header{Height: 1}})
	ctx := app.NewContext(false, abci.Header{Height: 1})

	// create validator & self delegate minSelfDelegate CET
	valAddr := sdk.ValAddress(addr)
	minSelfDelegate := app.stakingXKeeper.GetParams(ctx).MinSelfDelegation
	createValMsg := testutil.NewMsgCreateValidatorBuilder(valAddr, pk).
		MinSelfDelegation(minSelfDelegate.Int64()).SelfDelegation(minSelfDelegate.Int64()).
		Build()
	createValTx := newStdTxBuilder().
		Msgs(createValMsg).GasAndFee(1000000, 100).AccNumSeqKey(0, 0, sk).Build()
	createValResult := app.Deliver(createValTx)
	require.Equal(t, sdk.CodeOK, createValResult.Code)

	app.EndBlock(abci.RequestEndBlock{Height: 1})
	app.Commit()

	//next block
	app.BeginBlock(abci.RequestBeginBlock{Header: abci.Header{Height: app.LastBlockHeight() + 1}})
	ctx = app.NewContext(false, abci.Header{Height: app.LastBlockHeight() + 1})

	exportState, valset, err := app.ExportAppStateAndValidators(true, []string{})
	require.Nil(t, err)

	var appState GenesisState
	err = app.cdc.UnmarshalJSON(exportState, &appState)
	require.Nil(t, err)

	val := appState.StakingData.Validators
	require.Equal(t, pk, valset[0].PubKey)
	require.Equal(t, val[0].ConsensusPower(), valset[0].Power)

	valAcc := app.accountKeeper.GetAccount(ctx, addr)
	require.Equal(t, sdk.NewDec(0), appState.DistrData.FeePool.CommunityPool.AmountOf("cet"))
	require.Equal(t, cetToken().GetTotalSupply()-minSelfDelegate.Int64()-100, valAcc.GetCoins().AmountOf("cet").Int64())

	//TODO:
	//feeCollectAccount := getFeeCollectAccount(&appState)
	//require.NotNil(t, feeCollectAccount)
	//require.Equal(t, sdk.NewInt(100).Int64(), feeCollectAccount.Coins.AmountOf(dex.DefaultBondDenom).Int64())
}

//func getFeeCollectAccount(gs *GenesisState) *genaccounts.GenesisAccount {
//	for _, acc := range gs.Accounts {
//		if acc.ModuleName == distribution.ModuleName {
//			return &acc
//		}
//	}
//
//	return nil
//}

func TestExportValidatorsUpdateRestore(t *testing.T) {
	sk, pk, addr := testutil.KeyPubAddr()
	amount := cetToken().GetTotalSupply()

	acc := auth.BaseAccount{Address: addr, Coins: dex.NewCetCoins(amount)}

	// init app1
	app1 := initApp(func(genState *GenesisState) {
		addGenesisAccounts(genState, acc)
		genState.StakingXData.Params.MinSelfDelegation = sdk.NewInt(1e8)
	})

	app1.BeginBlock(abci.RequestBeginBlock{Header: abci.Header{Height: 1}})
	ctx := app1.NewContext(false, abci.Header{Height: 1})

	// create validator & self delegate minSelfDelegate CET
	valAddr := sdk.ValAddress(addr)
	minSelfDelegate := app1.stakingXKeeper.GetParams(ctx).MinSelfDelegation
	createValMsg := testutil.NewMsgCreateValidatorBuilder(valAddr, pk).
		MinSelfDelegation(minSelfDelegate.Int64()).SelfDelegation(minSelfDelegate.Int64()).
		Build()
	createValTx := newStdTxBuilder().
		Msgs(createValMsg).GasAndFee(1000000, 100).AccNumSeqKey(0, 0, sk).Build()
	createValResult := app1.Deliver(createValTx)
	require.Equal(t, sdk.CodeOK, createValResult.Code)

	app1.EndBlock(abci.RequestEndBlock{Height: 1})
	app1.Commit()

	//next block
	app1.BeginBlock(abci.RequestBeginBlock{Header: abci.Header{Height: app1.LastBlockHeight() + 1}})
	ctx = app1.NewContext(false, abci.Header{Height: app1.LastBlockHeight() + 1})

	exportState1 := app1.ExportGenesisState(ctx)

	// restore & reexport
	app2 := initAppWithValidators(exportState1)
	ctx2 := app2.NewContext(false, abci.Header{Height: app2.LastBlockHeight()})
	exportState2 := app2.ExportGenesisState(ctx2)

	// check
	json1, err1 := codec.MarshalJSONIndent(app1.cdc, exportState1)
	json2, err2 := codec.MarshalJSONIndent(app2.cdc, exportState2)
	require.Nil(t, err1)
	require.Nil(t, err2)
	require.Equal(t, json1, json2)

}
