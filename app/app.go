package app

import (
	"encoding/json"
	"io"
	"os"

	abci "github.com/tendermint/tendermint/abci/types"
	cmn "github.com/tendermint/tendermint/libs/common"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/libs/log"

	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	distrclient "github.com/cosmos/cosmos-sdk/x/distribution/client"
	"github.com/cosmos/cosmos-sdk/x/genaccounts"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/cosmos/cosmos-sdk/x/gov"
	"github.com/cosmos/cosmos-sdk/x/params"
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/cosmos/cosmos-sdk/x/supply"

	"github.com/coinexchain/dex/modules/asset"
	"github.com/coinexchain/dex/modules/authx"
	"github.com/coinexchain/dex/modules/bancorlite"
	"github.com/coinexchain/dex/modules/bankx"
	"github.com/coinexchain/dex/modules/distributionx"
	"github.com/coinexchain/dex/modules/incentive"
	"github.com/coinexchain/dex/modules/market"
	"github.com/coinexchain/dex/modules/msgqueue"
	"github.com/coinexchain/dex/modules/stakingx"
	"github.com/coinexchain/dex/modules/supplyx"
)

const (
	appName = "CoinExChainApp"
	// DefaultKeyPass contains the default key password for genesis transactions
	DefaultKeyPass = "12345678"
)

// default home directories for expected binaries
var (
	// default home directories for cetcli
	DefaultCLIHome = os.ExpandEnv("$HOME/.cetcli")

	// default home directories for cetd
	DefaultNodeHome = os.ExpandEnv("$HOME/.cetd")

	// The ModuleBasicManager is in charge of setting up basic,
	// non-dependant module elements, such as codec registration
	// and genesis verification.
	ModuleBasics OrderedBasicManager

	// account permissions
	maccPerms = map[string][]string{
		auth.FeeCollectorName:     {supply.Basic},
		distr.ModuleName:          {supply.Basic},
		staking.BondedPoolName:    {supply.Burner, supply.Staking},
		staking.NotBondedPoolName: {supply.Burner, supply.Staking},
		gov.ModuleName:            {supply.Burner},
		authx.ModuleName:          {supply.Basic},
		asset.ModuleName:          {supply.Burner, supply.Minter},
	}
)

func init() {
	modules := []module.AppModuleBasic{
		genaccounts.AppModuleBasic{},
		genutil.AppModuleBasic{},
		params.AppModuleBasic{},
		authx.AppModuleBasic{}, //before `bank` to override `/bank/balances/{address}`
		bankx.AppModuleBasic{},
		bank.AppModuleBasic{},
		distr.AppModuleBasic{},
		supply.AppModuleBasic{},
		AuthModuleBasic{},
		stakingx.AppModuleBasic{}, //before `staking` to override `cetcli q staking pool` command
		StakingModuleBasic{},
		SlashingModuleBasic{},
		CrisisModuleBasic{},
		GovModuleBasic{gov.NewAppModuleBasic(paramsclient.ProposalHandler, distrclient.ProposalHandler)},
		distributionx.AppModuleBasic{},
		incentive.AppModuleBasic{},
		asset.AppModuleBasic{},
		market.AppModuleBasic{},
		bancorlite.AppModuleBasic{},
	}

	ModuleBasics = NewOrderedBasicManager(modules)
}

// custom tx codec
func MakeCodec() *codec.Codec {
	var cdc = codec.New()
	ModuleBasics.RegisterCodec(cdc)
	sdk.RegisterCodec(cdc)
	codec.RegisterCrypto(cdc)
	return cdc
}

// Extended ABCI application
type CetChainApp struct {
	*bam.BaseApp
	cdc *codec.Codec

	invCheckPeriod uint

	// keys to access the substores
	keyMain      *sdk.KVStoreKey
	keyAccount   *sdk.KVStoreKey
	keyAccountX  *sdk.KVStoreKey
	keySupply    *sdk.KVStoreKey
	keyStaking   *sdk.KVStoreKey
	tkeyStaking  *sdk.TransientStoreKey
	keySlashing  *sdk.KVStoreKey
	keyDistr     *sdk.KVStoreKey
	tkeyDistr    *sdk.TransientStoreKey
	keyGov       *sdk.KVStoreKey
	keyParams    *sdk.KVStoreKey
	tkeyParams   *sdk.TransientStoreKey
	keyAsset     *sdk.KVStoreKey
	keyMarket    *sdk.KVStoreKey
	keyBancor    *sdk.KVStoreKey
	keyIncentive *sdk.KVStoreKey

	// Manage getting and setting accounts
	accountKeeper   auth.AccountKeeper
	accountXKeeper  authx.AccountXKeeper
	bankKeeper      bank.BaseKeeper
	bankxKeeper     bankx.Keeper // TODO rename to bankXKeeper
	supplyKeeper    supply.Keeper
	stakingKeeper   staking.Keeper
	stakingXKeeper  stakingx.Keeper
	slashingKeeper  slashing.Keeper
	distrKeeper     distr.Keeper
	distrxKeeper    distributionx.Keeper
	govKeeper       gov.Keeper
	crisisKeeper    crisis.Keeper
	incentiveKeeper incentive.Keeper
	assetKeeper     asset.Keeper
	tokenKeeper     asset.TokenKeeper
	paramsKeeper    params.Keeper
	marketKeeper    market.Keeper
	bancorKeeper    bancorlite.Keeper
	msgQueProducer  msgqueue.Producer

	// the module manager
	mm *module.Manager
}

// NewCetChainApp returns a reference to an initialized CetChainApp.
func NewCetChainApp(logger log.Logger, db dbm.DB, traceStore io.Writer, loadLatest bool,
	invCheckPeriod uint, baseAppOptions ...func(*bam.BaseApp)) *CetChainApp {

	cdc := MakeCodec()

	bApp := bam.NewBaseApp(appName, logger, db, auth.DefaultTxDecoder(cdc), baseAppOptions...)
	bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetAppVersion(version.Version)

	app := newCetChainApp(bApp, cdc, invCheckPeriod)
	app.initKeepers(invCheckPeriod)
	app.InitModules()
	app.mountStores()

	ah := authx.NewAnteHandler(app.accountKeeper, app.supplyKeeper, app.accountXKeeper,
		newAnteHelper(app.accountXKeeper, app.stakingXKeeper))

	app.SetInitChainer(app.initChainer)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetAnteHandler(ah)
	app.SetEndBlocker(app.EndBlocker)

	if loadLatest {
		err := app.LoadLatestVersion(app.keyMain)
		if err != nil {
			cmn.Exit(err.Error())
		}
	}

	return app
}

func newCetChainApp(bApp *bam.BaseApp, cdc *codec.Codec, invCheckPeriod uint) *CetChainApp {
	return &CetChainApp{
		BaseApp:        bApp,
		cdc:            cdc,
		invCheckPeriod: invCheckPeriod,
		keyMain:        sdk.NewKVStoreKey(bam.MainStoreKey),
		keyAccount:     sdk.NewKVStoreKey(auth.StoreKey),
		keyAccountX:    sdk.NewKVStoreKey(authx.StoreKey),
		keySupply:      sdk.NewKVStoreKey(supply.StoreKey),
		keyStaking:     sdk.NewKVStoreKey(staking.StoreKey),
		tkeyStaking:    sdk.NewTransientStoreKey(staking.TStoreKey),
		keyDistr:       sdk.NewKVStoreKey(distr.StoreKey),
		tkeyDistr:      sdk.NewTransientStoreKey(distr.TStoreKey),
		keySlashing:    sdk.NewKVStoreKey(slashing.StoreKey),
		keyGov:         sdk.NewKVStoreKey(gov.StoreKey),
		keyParams:      sdk.NewKVStoreKey(params.StoreKey),
		tkeyParams:     sdk.NewTransientStoreKey(params.TStoreKey),
		keyAsset:       sdk.NewKVStoreKey(asset.StoreKey),
		keyMarket:      sdk.NewKVStoreKey(market.StoreKey),
		keyBancor:      sdk.NewKVStoreKey(bancorlite.StoreKey),
		keyIncentive:   sdk.NewKVStoreKey(incentive.StoreKey),
	}
}

func (app *CetChainApp) initKeepers(invCheckPeriod uint) {
	app.paramsKeeper = params.NewKeeper(app.cdc, app.keyParams, app.tkeyParams, params.DefaultCodespace)
	app.msgQueProducer = msgqueue.NewProducer()
	// define the accountKeeper
	app.accountKeeper = auth.NewAccountKeeper(
		app.cdc,
		app.keyAccount,
		app.paramsKeeper.Subspace(auth.DefaultParamspace),
		auth.ProtoBaseAccount,
	)
	// add handlers
	app.bankKeeper = bank.NewBaseKeeper(
		app.accountKeeper,
		app.paramsKeeper.Subspace(bank.DefaultParamspace),
		bank.DefaultCodespace,
	)

	app.supplyKeeper = supply.NewKeeper(app.cdc, app.keySupply, app.accountKeeper,
		app.bankKeeper, supply.DefaultCodespace, maccPerms)

	var stakingKeeper staking.Keeper

	app.distrKeeper = distr.NewKeeper(
		app.cdc,
		app.keyDistr,
		app.paramsKeeper.Subspace(distr.DefaultParamspace),
		&stakingKeeper,
		app.supplyKeeper,
		distr.DefaultCodespace,
		auth.FeeCollectorName,
	)
	supplyxKeeper := supplyx.NewKeeper(app.supplyKeeper, app.distrKeeper)

	stakingKeeper = staking.NewKeeper(
		app.cdc,
		app.keyStaking, app.tkeyStaking,
		supplyxKeeper,
		//app.supplyKeeper,
		app.paramsKeeper.Subspace(staking.DefaultParamspace),
		staking.DefaultCodespace,
	)

	// register the proposal types
	govRouter := gov.NewRouter()
	govRouter.AddRoute(gov.RouterKey, gov.ProposalHandler).
		AddRoute(params.RouterKey, params.NewParamChangeProposalHandler(app.paramsKeeper)).
		AddRoute(distr.RouterKey, distr.NewCommunityPoolSpendProposalHandler(app.distrKeeper))

	app.govKeeper = gov.NewKeeper(
		app.cdc,
		app.keyGov,
		app.paramsKeeper, app.paramsKeeper.Subspace(gov.DefaultParamspace),
		//app.supplyKeeper,
		supplyxKeeper,
		&stakingKeeper,
		gov.DefaultCodespace,
		govRouter,
	)

	app.crisisKeeper = crisis.NewKeeper(
		app.paramsKeeper.Subspace(crisis.DefaultParamspace),
		invCheckPeriod,
		app.supplyKeeper,
		auth.FeeCollectorName,
	)

	// cet keepers
	app.accountXKeeper = authx.NewKeeper(
		app.cdc,
		app.keyAccountX,
		app.paramsKeeper.Subspace(authx.DefaultParamspace),
		app.supplyKeeper,
		app.accountKeeper,
	)

	app.slashingKeeper = slashing.NewKeeper(
		app.cdc,
		app.keySlashing,
		//app.stakingXKeeper,
		&stakingKeeper,
		app.paramsKeeper.Subspace(slashing.DefaultParamspace),
		slashing.DefaultCodespace,
	)
	app.incentiveKeeper = incentive.NewKeeper(
		app.cdc, app.keyIncentive,
		app.paramsKeeper.Subspace(incentive.DefaultParamspace),
		app.bankKeeper,
		app.supplyKeeper,
		auth.FeeCollectorName,
	)
	app.tokenKeeper = asset.NewBaseTokenKeeper(
		app.cdc, app.keyAsset,
	)
	app.bankxKeeper = bankx.NewKeeper(
		app.paramsKeeper.Subspace(bankx.DefaultParamspace),
		app.accountXKeeper, app.bankKeeper, app.accountKeeper,
		app.tokenKeeper,
		app.supplyKeeper,
		app.msgQueProducer,
	)
	app.distrxKeeper = distributionx.NewKeeper(
		app.bankxKeeper,
		app.distrKeeper,
	)
	app.assetKeeper = asset.NewBaseKeeper(
		app.cdc,
		app.keyAsset,
		app.paramsKeeper.Subspace(asset.DefaultParamspace),
		app.bankxKeeper,
		app.supplyKeeper,
	)
	app.stakingXKeeper = stakingx.NewKeeper(
		app.paramsKeeper.Subspace(stakingx.DefaultParamspace),
		app.assetKeeper,
		&stakingKeeper,
		app.distrKeeper,
		app.accountKeeper,
		app.bankxKeeper,
		app.supplyKeeper,
		auth.FeeCollectorName,
	)
	app.marketKeeper = market.NewBaseKeeper(
		app.keyMarket,
		app.tokenKeeper,
		app.bankxKeeper,
		app.cdc,
		app.msgQueProducer,
		app.paramsKeeper.Subspace(market.StoreKey),
	)
	app.bancorKeeper = bancorlite.NewBaseKeeper(bancorlite.NewBancorInfoKeeper(app.keyBancor, app.cdc), app.bankxKeeper, app.assetKeeper, app.marketKeeper)
	// register the staking hooks
	// NOTE: The stakingKeeper above is passed by reference, so that it can be
	// modified like below:
	app.stakingKeeper = *stakingKeeper.SetHooks(
		staking.NewMultiStakingHooks(app.distrKeeper.Hooks(), app.slashingKeeper.Hooks()))
}

func (app *CetChainApp) InitModules() {

	modules := []module.AppModule{
		genaccounts.NewAppModule(app.accountKeeper),
		auth.NewAppModule(app.accountKeeper),
		authx.NewAppModule(app.accountXKeeper),
		bank.NewAppModule(app.bankKeeper, app.accountKeeper),
		bankx.NewAppModule(app.bankxKeeper),
		crisis.NewAppModule(app.crisisKeeper),
		incentive.NewAppModule(app.incentiveKeeper),
		supply.NewAppModule(app.supplyKeeper, app.accountKeeper),
		distr.NewAppModule(app.distrKeeper, app.supplyKeeper),
		distributionx.NewAppModule(app.distrxKeeper),
		gov.NewAppModule(app.govKeeper, app.supplyKeeper),
		slashing.NewAppModule(app.slashingKeeper, app.stakingKeeper),
		staking.NewAppModule(app.stakingKeeper, app.distrKeeper, app.accountKeeper, app.supplyKeeper),
		stakingx.NewAppModule(app.stakingXKeeper),
		asset.NewAppModule(app.assetKeeper),
		market.NewAppModule(app.marketKeeper),
		bancorlite.NewAppModule(app.bancorKeeper),
		genutil.NewAppModule(app.accountKeeper, app.stakingKeeper, app.BaseApp.DeliverTx),
	}

	app.mm = module.NewManager(modules...)
	// During begin block slashing happens after distr.BeginBlocker so that
	// there is nothing left over in the validator fee pool, so as to keep the
	// CanWithdrawInvariant invariant.
	app.mm.SetOrderBeginBlockers(market.ModuleName, incentive.ModuleName, distr.ModuleName, slashing.ModuleName)

	app.mm.SetOrderEndBlockers(gov.ModuleName, staking.ModuleName, authx.ModuleName, market.ModuleName, crisis.ModuleName)

	initGenesisOrder := []string{
		genaccounts.ModuleName,
		distr.ModuleName,
		staking.ModuleName,
		auth.ModuleName,
		bank.ModuleName,
		slashing.ModuleName,
		gov.ModuleName,
		supply.ModuleName,
		authx.ModuleName,
		bankx.ModuleName,
		incentive.ModuleName,
		stakingx.ModuleName,
		asset.ModuleName,
		market.ModuleName,
		bancorlite.ModuleName,
		crisis.ModuleName,
		genutil.ModuleName, //call DeliverGenTxs in genutil at last
	}

	// genutils must occur after staking so that pools are properly
	// initialized with tokens from genesis accounts.
	app.mm.SetOrderInitGenesis(initGenesisOrder...)

	exportGenesisOrder := initGenesisOrder
	app.mm.SetOrderExportGenesis(exportGenesisOrder...)

	app.crisisKeeper.RegisterRoute(authx.ModuleName, "pre-total-supply", authx.PreTotalSupplyInvariant(app.accountXKeeper))
	app.mm.RegisterInvariants(&app.crisisKeeper)

	//crisis module should be reset since invariants has been registered to crisis keeper
	app.replaceEmptyCrisisModule(&modules)

	registerRoutesWithOrder(modules, app.Router(), app.QueryRouter())
}

func (app *CetChainApp) replaceEmptyCrisisModule(modules *[]module.AppModule) {
	crisisWithInvariants := crisis.NewAppModule(app.crisisKeeper)

	app.mm.Modules[crisis.ModuleName] = crisisWithInvariants

	for i, module := range *modules {
		if module.Name() == crisis.ModuleName {
			(*modules)[i] = crisisWithInvariants
		}
	}
}

func registerRoutesWithOrder(modules []module.AppModule, router sdk.Router, queryRouter sdk.QueryRouter) {
	for _, module := range modules {
		if module.Route() != "" {
			router.AddRoute(module.Route(), module.NewHandler())
		}
		if module.QuerierRoute() != "" {
			queryRouter.AddRoute(module.QuerierRoute(), module.NewQuerierHandler())
		}
	}
}

// initialize BaseApp
func (app *CetChainApp) mountStores() {
	app.MountStores(app.keyMain, app.keyAccount, app.keySupply, app.keyStaking, app.keyDistr,
		app.keySlashing, app.keyGov, app.keyParams,
		app.tkeyParams, app.tkeyStaking, app.tkeyDistr,
		app.keyAccountX, app.keyAsset, app.keyMarket, app.keyIncentive,
		app.keyBancor,
	)
}

// application updates every end block
func (app *CetChainApp) BeginBlocker(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock {
	PubMsgs = make([]PubMsg, 0, 10000)
	ret := app.mm.BeginBlock(ctx, req)
	ret.Events = FilterMsgsOnlyKafka(ret.Events)
	return ret
}

func (app *CetChainApp) DeliverTx(req abci.RequestDeliverTx) abci.ResponseDeliverTx {
	ret := app.BaseApp.DeliverTx(req)
	if ret.Code == uint32(sdk.CodeOK) {
		ret.Events = FilterMsgsOnlyKafka(ret.Events)
	} else {
		ret.Events = RemoveMsgsOnlyKafka(ret.Events)
	}
	return ret
}

// application updates every end block
// nolint: unparam
func (app *CetChainApp) EndBlocker(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	ret := app.mm.EndBlock(ctx, req)
	ret.Events = FilterMsgsOnlyKafka(ret.Events)
	return ret
}

func (app *CetChainApp) Commit() abci.ResponseCommit {
	for _, msg := range PubMsgs {
		app.msgQueProducer.SendMsg(msg.Key, msg.Value)
	}
	return app.BaseApp.Commit()
}

// custom logic for coindex initialization
func (app *CetChainApp) initChainer(ctx sdk.Context, req abci.RequestInitChain) abci.ResponseInitChain {
	var genesisState map[string]json.RawMessage
	app.cdc.MustUnmarshalJSON(req.AppStateBytes, &genesisState)

	if err := ModuleBasics.ValidateGenesis(genesisState); err != nil {
		panic(err)
	}

	return app.mm.InitGenesis(ctx, genesisState)
}

// load a particular height
func (app *CetChainApp) LoadHeight(height int64) error {
	return app.LoadVersion(height, app.keyMain)
}
