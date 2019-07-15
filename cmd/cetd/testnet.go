package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/cosmos/cosmos-sdk/x/staking"

	"github.com/coinexchain/dex/app"
	"github.com/coinexchain/dex/modules/asset"
	"github.com/coinexchain/dex/modules/authx"
	"github.com/coinexchain/dex/modules/stakingx"
	dex "github.com/coinexchain/dex/types"
)

const nodeDirPerm = 0755

var (
	flagNodeDirPrefix     = "node-dir-prefix"
	flagNumValidators     = "v"
	flagOutputDir         = "output-dir"
	flagNodeDaemonHome    = "node-daemon-home"
	flagNodeCliHome       = "node-cli-home"
	flagStartingIPAddress = "starting-ip-address"

	testnetTokenSupply       = int64(588788547005740000)
	testnetMinSelfDelegation = int64(10000e8)
)

type testnetNodeInfo struct {
	nodeID    string
	valPubKey crypto.PubKey
	acc       app.GenesisAccount
	genFile   string
}

// get cmd to initialize all files for tendermint testnet and application
func testnetCmd(ctx *server.Context, cdc *codec.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "testnet",
		Short: "Initialize files for a Cetd testnet",
		Long: `testnet will create "v" number of directories and populate each with
necessary files (private validator, genesis, config, etc.).

Note, strict routability for addresses is turned off in the config file.

Example:
	cetd testnet --v 4 --output-dir ./output --starting-ip-address 192.168.10.2
	`,
		RunE: func(_ *cobra.Command, _ []string) error {
			config := ctx.Config
			return initTestnet(config, cdc)
		},
	}

	prepareFlagsForTestnetCmd(cmd)

	return cmd
}

func prepareFlagsForTestnetCmd(cmd *cobra.Command) {
	cmd.Flags().Int(flagNumValidators, 4,
		"Number of validators to initialize the testnet with",
	)
	cmd.Flags().StringP(flagOutputDir, "o", "./mytestnet",
		"Directory to store initialization data for the testnet",
	)
	cmd.Flags().String(flagNodeDirPrefix, "node",
		"Prefix the directory name for each node with (node results in node0, node1, ...)",
	)
	cmd.Flags().String(flagNodeDaemonHome, "cetd",
		"Home directory of the node's daemon configuration",
	)
	cmd.Flags().String(flagNodeCliHome, "cetcli",
		"Home directory of the node's cli configuration",
	)
	cmd.Flags().String(flagStartingIPAddress, "192.168.0.1",
		"Starting IP address (192.168.0.1 results in persistent peers list ID0@192.168.0.1:46656, ID1@192.168.0.2:46656, ...)")

	cmd.Flags().String(
		client.FlagChainID, "", "genesis file chain-id, if left blank will be randomly created",
	)
	cmd.Flags().String(
		server.FlagMinGasPrices, fmt.Sprintf("%s%s", authx.DefaultMinGasPriceLimit, dex.DefaultBondDenom), //20sato.CET
		"Minimum gas prices to accept for transactions; All fees in a tx must meet this minimum (e.g. 20cet)",
	)
}

func initTestnet(config *tmconfig.Config, cdc *codec.Codec) error {
	outDir := viper.GetString(flagOutputDir)
	numValidators := viper.GetInt(flagNumValidators)

	chainID := viper.GetString(client.FlagChainID)
	if chainID == "" {
		chainID = "chain-" + cmn.RandStr(6)
	}

	nodeIDs := make([]string, numValidators)
	valPubKeys := make([]crypto.PubKey, numValidators)
	accs := make([]app.GenesisAccount, numValidators)
	genFiles := make([]string, numValidators)

	dexConfig := srvconfig.DefaultConfig()
	dexConfig.MinGasPrices = viper.GetString(server.FlagMinGasPrices)

	// generate private keys, node IDs, and initial transactions
	for i := 0; i < numValidators; i++ {
		nodeInfo, err := initTestnetNode(config, cdc, outDir, chainID, i)
		if err != nil {
			return err
		}

		nodeIDs[i] = nodeInfo.nodeID
		valPubKeys[i] = nodeInfo.valPubKey
		accs[i] = nodeInfo.acc
		genFiles[i] = nodeInfo.genFile
	}

	if err := initGenFiles(cdc, chainID, accs, genFiles, numValidators); err != nil {
		return err
	}

	err := collectGenFiles(
		cdc, config, chainID, nodeIDs, valPubKeys, numValidators,
		outDir, viper.GetString(flagNodeDirPrefix), viper.GetString(flagNodeDaemonHome),
		nil, // TODO
	)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully initialized %d node directories\n", numValidators)
	return nil
}

func initTestnetNode(config *tmconfig.Config, cdc *codec.Codec,
	outDir, chainID string, i int,
) (testnetNodeInfo, error) {

	nodeDirName := fmt.Sprintf("%s%d", viper.GetString(flagNodeDirPrefix), i)
	nodeDaemonHomeName := viper.GetString(flagNodeDaemonHome)
	nodeCliHomeName := viper.GetString(flagNodeCliHome)
	nodeDir := filepath.Join(outDir, nodeDirName, nodeDaemonHomeName)
	clientDir := filepath.Join(outDir, nodeDirName, nodeCliHomeName)
	gentxsDir := filepath.Join(outDir, "gentxs")

	config.SetRoot(nodeDir)

	err := mkNodeHomeDirs(outDir, nodeDir, clientDir)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return testnetNodeInfo{}, err
	}

	config.Moniker = nodeDirName
	adjustBlockCommitSpeed(config)

	ip, err := getIP(i, viper.GetString(flagStartingIPAddress))
	if err != nil {
		_ = os.RemoveAll(outDir)
		return testnetNodeInfo{}, err
	}

	nodeID, valPubKey, err := genutil.InitializeNodeValidatorFiles(config)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return testnetNodeInfo{}, err
	}

	memo := fmt.Sprintf("%s@%s:26656", nodeID, ip)
	genFile := config.GenesisFile()

	buf := bufio.NewReader(os.Stdin) // TODO
	prompt := fmt.Sprintf(
		"Password for account '%s' (default %s):", nodeDirName, app.DefaultKeyPass,
	)

	keyPass, err := client.GetPassword(prompt, buf)
	if err != nil && keyPass != "" {
		// An error was returned that either failed to read the password from
		// STDIN or the given password is not empty but failed to meet minimum
		// length requirements.
		return testnetNodeInfo{}, err
	}

	if keyPass == "" {
		keyPass = app.DefaultKeyPass
	}

	addr, secret, err := server.GenerateSaveCoinKey(clientDir, nodeDirName, keyPass, true)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return testnetNodeInfo{}, err
	}

	info := map[string]string{"secret": secret}

	cliPrint, err := json.Marshal(info)
	if err != nil {
		return testnetNodeInfo{}, err
	}

	// save private key seed words
	err = writeFile(fmt.Sprintf("%v.json", "key_seed"), clientDir, cliPrint)
	if err != nil {
		return testnetNodeInfo{}, err
	}

	minSelfDel := stakingx.DefaultParams().MinSelfDelegation.Quo(sdk.NewInt(100))
	accStakingTokens := minSelfDel.MulRaw(10)
	acc := app.GenesisAccount{
		Address: addr,
		Coins: sdk.Coins{
			sdk.NewCoin(dex.DefaultBondDenom, accStakingTokens),
		},
	}

	msg := staking.NewMsgCreateValidator(
		sdk.ValAddress(addr),
		valPubKey,
		sdk.NewCoin(dex.DefaultBondDenom, minSelfDel),
		staking.NewDescription(nodeDirName, "", "", ""),
		staking.NewCommissionRates(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec()),
		minSelfDel,
	)
	kb, err := keys.NewKeyBaseFromDir(clientDir)
	if err != nil {
		return testnetNodeInfo{}, err
	}
	tx := auth.NewStdTx([]sdk.Msg{msg}, auth.StdFee{}, []auth.StdSignature{}, memo)
	txBldr := auth.NewTxBuilderFromCLI().WithChainID(chainID).WithMemo(memo).WithKeybase(kb)

	signedTx, err := txBldr.SignStdTx(nodeDirName, app.DefaultKeyPass, tx, false)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return testnetNodeInfo{}, err
	}

	txBytes, err := cdc.MarshalJSON(signedTx)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return testnetNodeInfo{}, err
	}

	// gather gentxs folder
	err = writeFile(fmt.Sprintf("%v.json", nodeDirName), gentxsDir, txBytes)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return testnetNodeInfo{}, err
	}

	configFilePath := filepath.Join(nodeDir, "config/cetd.toml")
	srvconfig.WriteConfigFile(configFilePath, srvconfig.DefaultConfig())
	return testnetNodeInfo{
		nodeID:    nodeID,
		valPubKey: valPubKey,
		acc:       acc,
		genFile:   genFile,
	}, nil
}

func mkNodeHomeDirs(outDir, nodeDir, clientDir string) error {
	err := os.MkdirAll(filepath.Join(nodeDir, "config"), nodeDirPerm)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return err
	}

	err = os.MkdirAll(clientDir, nodeDirPerm)
	if err != nil {
		_ = os.RemoveAll(outDir)
		return err
	}

	return nil
}

func initGenFiles(
	cdc *codec.Codec, chainID string, accs []app.GenesisAccount,
	genFiles []string, numValidators int,
) error {

	appGenState := app.NewDefaultGenesisState()
	appGenState.StakingXData.Params.MinSelfDelegation = sdk.NewInt(testnetMinSelfDelegation)
	addCetTokenForTesting(&appGenState, testnetTokenSupply, accs[0].Address)

	accs = assureTokenDistributionInGenesis(accs, testnetTokenSupply)
	appGenState.Accounts = accs

	appGenStateJSON, err := codec.MarshalJSONIndent(cdc, appGenState)
	if err != nil {
		return err
	}

	genDoc := types.GenesisDoc{
		ChainID:    chainID,
		AppState:   appGenStateJSON,
		Validators: nil,
	}

	// generate empty genesis files for each validator and save
	for i := 0; i < numValidators; i++ {
		if err := genDoc.SaveAs(genFiles[i]); err != nil {
			return err
		}
	}

	return nil
}

func assureTokenDistributionInGenesis(accs []app.GenesisAccount, testnetSupply int64) []app.GenesisAccount {
	var distributedTokens int64
	for _, acc := range accs {
		distributedTokens += acc.Coins[0].Amount.Int64()
	}

	if testnetSupply > distributedTokens {
		accs = append(accs, app.GenesisAccount{
			Address: sdk.AccAddress(crypto.AddressHash([]byte("left_tokens"))),
			Coins: sdk.Coins{
				sdk.NewCoin(dex.DefaultBondDenom, sdk.NewInt(testnetSupply-distributedTokens)),
			},
		})
	}
	return accs
}

func addCetTokenForTesting(appGenState *app.GenesisState, tokenTotalSupply int64, cetOwner sdk.AccAddress) {
	baseToken, _ := asset.NewToken("CoinEx Chain Native Token",
		"cet",
		tokenTotalSupply,
		cetOwner,
		false,
		true,
		false,
		false,
		"www.coinex.org",
		"A public chain built for the decentralized exchange",
	)

	var token asset.Token = baseToken
	appGenState.AssetData.Tokens = []asset.Token{token}
}

func collectGenFiles(
	cdc *codec.Codec, config *tmconfig.Config, chainID string,
	nodeIDs []string, valPubKeys []crypto.PubKey,
	numValidators int, outputDir, nodeDirPrefix, nodeDaemonHome string,
	genAccIterator genutil.GenesisAccountsIterator,
) error {

	var appState json.RawMessage
	genTime := tmtime.Now()

	for i := 0; i < numValidators; i++ {
		nodeDirName := fmt.Sprintf("%s%d", nodeDirPrefix, i)
		nodeDir := filepath.Join(outputDir, nodeDirName, nodeDaemonHome)
		gentxsDir := filepath.Join(outputDir, "gentxs")
		moniker := nodeDirName
		config.Moniker = nodeDirName

		config.SetRoot(nodeDir)

		nodeID, valPubKey := nodeIDs[i], valPubKeys[i]
		initCfg := genutil.NewInitConfig(chainID, gentxsDir, moniker, nodeID, valPubKey)

		genDoc, err := types.GenesisDocFromFile(config.GenesisFile())
		if err != nil {
			return err
		}

		nodeAppState, err := genutil.GenAppStateFromConfig(cdc, config, initCfg, *genDoc, genAccIterator)
		if err != nil {
			return err
		}

		if appState == nil {
			// set the canonical application state (they should not differ)
			appState = nodeAppState
		}

		genFile := config.GenesisFile()

		// overwrite each validator's genesis file to have a canonical genesis time
		if err := genutil.ExportGenesisFileWithTime(genFile, chainID, nil, appState, genTime); err != nil {
			return err
		}
	}

	return nil
}

func getIP(i int, startingIPAddr string) (ip string, err error) {
	if len(startingIPAddr) == 0 {
		ip, err = server.ExternalIP()
		if err != nil {
			return "", err
		}
		return ip, nil
	}
	return calculateIP(startingIPAddr, i)
}

func writeFile(name string, dir string, contents []byte) error {
	writePath := filepath.Join(dir)
	file := filepath.Join(writePath, name)

	err := cmn.EnsureDir(writePath, 0700)
	if err != nil {
		return err
	}

	err = cmn.WriteFile(file, contents, 0600)
	if err != nil {
		return err
	}

	return nil
}

func calculateIP(ip string, i int) (string, error) {
	ipv4 := net.ParseIP(ip).To4()
	if ipv4 == nil {
		return "", fmt.Errorf("%v: non ipv4 address", ip)
	}

	for j := 0; j < i; j++ {
		ipv4[3]++
	}

	return ipv4.String(), nil
}
