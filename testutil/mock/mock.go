package mock

import (
	"bytes"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	"github.com/cosmos/cosmos-sdk/x/staking"

	"github.com/stretchr/testify/require"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/libs/log"

	tapp "github.com/terra-project/core/app"
	"github.com/terra-project/core/types"
	"github.com/terra-project/core/types/assets"
	"github.com/terra-project/core/x/budget"
	tdistr "github.com/terra-project/core/x/distribution"
	"github.com/terra-project/core/x/market"
	"github.com/terra-project/core/x/mint"
	"github.com/terra-project/core/x/oracle"
	"github.com/terra-project/core/x/pay"
	tslashing "github.com/terra-project/core/x/slashing"
	tstaking "github.com/terra-project/core/x/staking"
	"github.com/terra-project/core/x/treasury"
)

const chainID = ""

// App extends an ABCI application, but with most of its parameters exported.
// They are exported for convenience in creating helper functions, as object
// capabilities aren't needed for testing.
type App struct {
	*bam.BaseApp
	Cdc              *codec.Codec // Cdc is public since the codec is passed into the module anyways
	KeyMain          *sdk.KVStoreKey
	KeyAccount       *sdk.KVStoreKey
	KeyFeeCollection *sdk.KVStoreKey
	KeyParams        *sdk.KVStoreKey
	TkeyParams       *sdk.TransientStoreKey
	KeyStaking       *sdk.KVStoreKey
	TkeyStaking      *sdk.TransientStoreKey
	KeySlashing      *sdk.KVStoreKey
	KeyDistr         *sdk.KVStoreKey
	TkeyDistr        *sdk.TransientStoreKey
	KeyOracle        *sdk.KVStoreKey
	KeyTreasury      *sdk.KVStoreKey
	KeyMarket        *sdk.KVStoreKey
	KeyBudget        *sdk.KVStoreKey
	KeyMint          *sdk.KVStoreKey

	// TODO: Abstract this out from not needing to be auth specifically
	AccountKeeper       auth.AccountKeeper
	FeeCollectionKeeper auth.FeeCollectionKeeper
	ParamsKeeper        params.Keeper
	BankKeeper          bank.Keeper
	StakingKeeper       staking.Keeper
	SlashingKeeper      slashing.Keeper
	DistrKeeper         distr.Keeper
	CrisisKeeper        crisis.Keeper
	OracleKeeper        oracle.Keeper
	TreasuryKeeper      treasury.Keeper
	MarketKeeper        market.Keeper
	BudgetKeeper        budget.Keeper
	MintKeeper          mint.Keeper

	NotBondedTokens  sdk.Int
	GenesisAccounts  []auth.Account
	TotalCoinsSupply sdk.Coins
	t                *testing.T
}

// NewApp initialize the mock application for this module
func NewApp(t *testing.T) *App {

	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout)).With("module", "core/app")
	db := dbm.NewMemDB()
	cdc := createCodec()

	app := &App{
		BaseApp:          bam.NewBaseApp("mock", logger, db, auth.DefaultTxDecoder(cdc)),
		Cdc:              cdc,
		KeyMain:          sdk.NewKVStoreKey(bam.MainStoreKey),
		KeyAccount:       sdk.NewKVStoreKey(auth.StoreKey),
		KeyFeeCollection: sdk.NewKVStoreKey(auth.FeeStoreKey),
		KeyParams:        sdk.NewKVStoreKey(params.StoreKey),
		TkeyParams:       sdk.NewTransientStoreKey(params.TStoreKey),
		TotalCoinsSupply: sdk.NewCoins(),
		KeyStaking:       sdk.NewKVStoreKey(staking.StoreKey),
		TkeyStaking:      sdk.NewTransientStoreKey(staking.TStoreKey),
		KeyDistr:         sdk.NewKVStoreKey(distr.StoreKey),
		TkeyDistr:        sdk.NewTransientStoreKey(distr.TStoreKey),
		KeySlashing:      sdk.NewKVStoreKey(slashing.StoreKey),
		KeyOracle:        sdk.NewKVStoreKey(oracle.StoreKey),
		KeyTreasury:      sdk.NewKVStoreKey(treasury.StoreKey),
		KeyMarket:        sdk.NewKVStoreKey(market.StoreKey),
		KeyBudget:        sdk.NewKVStoreKey(budget.StoreKey),
		KeyMint:          sdk.NewKVStoreKey(mint.StoreKey),
		t:                t,
	}

	app.ParamsKeeper = params.NewKeeper(app.Cdc, app.KeyParams, app.TkeyParams)

	// Define the accountKeeper
	app.AccountKeeper = auth.NewAccountKeeper(
		app.Cdc,
		app.KeyAccount,
		app.ParamsKeeper.Subspace(auth.DefaultParamspace),
		auth.ProtoBaseAccount,
	)
	app.FeeCollectionKeeper = auth.NewFeeCollectionKeeper(
		app.Cdc,
		app.KeyFeeCollection,
	)

	app.BankKeeper = bank.NewBaseKeeper(
		app.AccountKeeper,
		app.ParamsKeeper.Subspace(bank.DefaultParamspace),
		bank.DefaultCodespace,
	)

	stakingKeeper := staking.NewKeeper(
		app.Cdc,
		app.KeyStaking, app.TkeyStaking,
		app.BankKeeper, app.ParamsKeeper.Subspace(staking.DefaultParamspace),
		staking.DefaultCodespace,
	)

	app.DistrKeeper = distr.NewKeeper(
		app.Cdc,
		app.KeyDistr,
		app.ParamsKeeper.Subspace(distr.DefaultParamspace),
		app.BankKeeper, &stakingKeeper, app.FeeCollectionKeeper,
		distr.DefaultCodespace,
	)

	app.CrisisKeeper = crisis.NewKeeper(
		app.ParamsKeeper.Subspace(crisis.DefaultParamspace),
		app.DistrKeeper,
		app.BankKeeper,
		app.FeeCollectionKeeper,
	)

	app.SlashingKeeper = slashing.NewKeeper(
		app.Cdc,
		app.KeySlashing,
		&stakingKeeper, app.ParamsKeeper.Subspace(slashing.DefaultParamspace),
		slashing.DefaultCodespace,
	)

	app.MintKeeper = mint.NewKeeper(
		app.Cdc,
		app.KeyMint,
		stakingKeeper,
		app.BankKeeper,
		app.AccountKeeper,
	)

	app.OracleKeeper = oracle.NewKeeper(
		app.Cdc,
		app.KeyOracle,
		app.MintKeeper,
		app.DistrKeeper,
		app.FeeCollectionKeeper,
		stakingKeeper.GetValidatorSet(),
		app.ParamsKeeper.Subspace(oracle.DefaultParamspace),
	)

	app.MarketKeeper = market.NewKeeper(
		app.Cdc,
		app.KeyMarket,
		app.OracleKeeper,
		app.MintKeeper,
		app.ParamsKeeper.Subspace(market.DefaultParamspace),
	)

	app.TreasuryKeeper = treasury.NewKeeper(
		app.Cdc,
		app.KeyTreasury,
		stakingKeeper.GetValidatorSet(),
		app.MintKeeper,
		app.MarketKeeper,
		app.ParamsKeeper.Subspace(treasury.DefaultParamspace),
	)

	app.BudgetKeeper = budget.NewKeeper(
		app.Cdc,
		app.KeyBudget,
		app.MarketKeeper,
		app.MintKeeper,
		app.TreasuryKeeper,
		stakingKeeper.GetValidatorSet(),
		app.ParamsKeeper.Subspace(budget.DefaultParamspace),
	)

	app.StakingKeeper = *stakingKeeper.SetHooks(tapp.NewStakingHooks(app.DistrKeeper.Hooks(), app.SlashingKeeper.Hooks()))

	bank.RegisterInvariants(&app.CrisisKeeper, app.AccountKeeper)
	distr.RegisterInvariants(&app.CrisisKeeper, app.DistrKeeper, app.StakingKeeper)
	staking.RegisterInvariants(&app.CrisisKeeper, app.StakingKeeper, app.FeeCollectionKeeper, app.DistrKeeper, app.AccountKeeper)

	app.Router().
		AddRoute(bank.RouterKey, pay.NewHandler(app.BankKeeper, app.TreasuryKeeper, app.FeeCollectionKeeper)).
		AddRoute(staking.RouterKey, staking.NewHandler(app.StakingKeeper)).
		AddRoute(distr.RouterKey, distr.NewHandler(app.DistrKeeper)).
		AddRoute(slashing.RouterKey, slashing.NewHandler(app.SlashingKeeper)).
		AddRoute(oracle.RouterKey, oracle.NewHandler(app.OracleKeeper)).
		AddRoute(budget.RouterKey, budget.NewHandler(app.BudgetKeeper)).
		AddRoute(market.RouterKey, market.NewHandler(app.MarketKeeper)).
		AddRoute(crisis.RouterKey, crisis.NewHandler(app.CrisisKeeper))

	app.QueryRouter().
		AddRoute(auth.QuerierRoute, auth.NewQuerier(app.AccountKeeper)).
		AddRoute(distr.QuerierRoute, distr.NewQuerier(app.DistrKeeper)).
		AddRoute(slashing.QuerierRoute, slashing.NewQuerier(app.SlashingKeeper, app.Cdc)).
		AddRoute(staking.QuerierRoute, staking.NewQuerier(app.StakingKeeper, app.Cdc)).
		AddRoute(treasury.QuerierRoute, treasury.NewQuerier(app.TreasuryKeeper)).
		AddRoute(market.QuerierRoute, market.NewQuerier(app.MarketKeeper)).
		AddRoute(oracle.QuerierRoute, oracle.NewQuerier(app.OracleKeeper)).
		AddRoute(budget.QuerierRoute, budget.NewQuerier(app.BudgetKeeper))

	app.MountStores(
		app.KeyMain, app.KeyAccount, app.KeyStaking, app.KeyDistr,
		app.KeySlashing, app.KeyFeeCollection, app.KeyParams,
		app.TkeyParams, app.TkeyStaking, app.TkeyDistr, app.KeyMarket,
		app.KeyOracle, app.KeyTreasury, app.KeyBudget, app.KeyMint,
	)

	app.SetInitChainer(app.InitChainer)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetAnteHandler(auth.NewAnteHandler(app.AccountKeeper, app.FeeCollectionKeeper))
	app.SetEndBlocker(app.EndBlocker)

	err := app.LoadLatestVersion(app.KeyMain)
	require.Nil(t, err)

	return app
}

func createCodec() *codec.Codec {
	cdc := codec.New()
	sdk.RegisterCodec(cdc)
	codec.RegisterCrypto(cdc)
	auth.RegisterCodec(cdc)
	pay.RegisterCodec(cdc)
	tstaking.RegisterCodec(cdc)
	tdistr.RegisterCodec(cdc)
	tslashing.RegisterCodec(cdc)
	types.RegisterCodec(cdc)
	oracle.RegisterCodec(cdc)
	budget.RegisterCodec(cdc)
	market.RegisterCodec(cdc)
	treasury.RegisterCodec(cdc)
	crisis.RegisterCodec(cdc)
	return cdc
}

// InitChainer performs custom logic for initialization.
// nolint: errcheck
func (app *App) InitChainer(ctx sdk.Context, _ abci.RequestInitChain) abci.ResponseInitChain {
	// Load the genesis accounts
	for _, genacc := range app.GenesisAccounts {
		acc := app.AccountKeeper.NewAccountWithAddress(ctx, genacc.GetAddress())
		acc.SetCoins(genacc.GetCoins())
		app.AccountKeeper.SetAccount(ctx, acc)
	}

	// initialize distribution (must happen before staking)
	distr.InitGenesis(ctx, app.DistrKeeper, distr.DefaultGenesisState())

	// load the initial staking information
	stakingData := staking.DefaultGenesisState()
	stakingData.Params.BondDenom = assets.MicroLunaDenom
	stakingData.Pool.NotBondedTokens = app.NotBondedTokens
	_, err := staking.InitGenesis(ctx, app.StakingKeeper, stakingData)
	require.Nil(app.t, err)

	auth.InitGenesis(ctx, app.AccountKeeper, app.FeeCollectionKeeper, auth.DefaultGenesisState())
	bank.InitGenesis(ctx, app.BankKeeper, bank.DefaultGenesisState())
	slashing.InitGenesis(ctx, app.SlashingKeeper, slashing.DefaultGenesisState(), stakingData.Validators.ToSDKValidators())
	crisis.InitGenesis(ctx, app.CrisisKeeper, crisis.DefaultGenesisState())
	treasury.InitGenesis(ctx, app.TreasuryKeeper, treasury.DefaultGenesisState())
	market.InitGenesis(ctx, app.MarketKeeper, market.DefaultGenesisState())
	// to prevent too long voting period
	budgetGenesisStatue := budget.DefaultGenesisState()
	budgetGenesisStatue.Params.VotePeriod = 10
	budget.InitGenesis(ctx, app.BudgetKeeper, budgetGenesisStatue)
	oracle.InitGenesis(ctx, app.OracleKeeper, oracle.DefaultGenesisState())

	// GetIssuance needs to be called once to read account balances to the store
	app.MintKeeper.GetIssuance(ctx, assets.MicroLunaDenom, sdk.ZeroInt())

	return abci.ResponseInitChain{}
}

// BeginBlocker application updates every end block
func (app *App) BeginBlocker(ctx sdk.Context, req abci.RequestBeginBlock) abci.ResponseBeginBlock {

	// distribute rewards for the previous block
	distr.BeginBlocker(ctx, req, app.DistrKeeper)

	// slash anyone who double signed.
	// NOTE: This should happen after distr.BeginBlocker so that
	// there is nothing left over in the validator fee pool,
	// so as to keep the CanWithdrawInvariant invariant.
	// TODO: This should really happen at EndBlocker.
	tags := slashing.BeginBlocker(ctx, req, app.SlashingKeeper)

	return abci.ResponseBeginBlock{
		Tags: tags.ToKVPairs(),
	}
}

func (app *App) assertRuntimeInvariants() {
	ctx := app.NewContext(false, abci.Header{Height: app.LastBlockHeight() + 1})
	app.assertRuntimeInvariantsOnContext(ctx)
}

func (app *App) assertRuntimeInvariantsOnContext(ctx sdk.Context) {
	start := time.Now()
	invarRoutes := app.CrisisKeeper.Routes()
	for _, ir := range invarRoutes {
		err := ir.Invar(ctx)
		require.Nil(app.t, err)
	}
	end := time.Now()
	diff := end.Sub(start)
	app.BaseApp.Logger().With("module", "invariants").Info("Asserted all invariants", "duration", diff)
}

// EndBlocker application updates every end block
func (app *App) EndBlocker(ctx sdk.Context, req abci.RequestEndBlock) abci.ResponseEndBlock {
	validatorUpdates, tags := staking.EndBlocker(ctx, app.StakingKeeper)

	oracleTags := oracle.EndBlocker(ctx, app.OracleKeeper)
	tags = append(tags, oracleTags...)

	budgetTags := budget.EndBlocker(ctx, app.BudgetKeeper)
	tags = append(tags, budgetTags...)

	treasuryTags := treasury.EndBlocker(ctx, app.TreasuryKeeper)
	tags = append(tags, treasuryTags...)

	app.assertRuntimeInvariants()

	return abci.ResponseEndBlock{
		ValidatorUpdates: validatorUpdates,
		Tags:             tags,
	}
}

// Type that combines an Address with the privKey and pubKey to that address
type AddrKeys struct {
	Address sdk.AccAddress
	PubKey  crypto.PubKey
	PrivKey crypto.PrivKey
}

func NewAddrKeys(address sdk.AccAddress, pubKey crypto.PubKey,
	privKey crypto.PrivKey) AddrKeys {

	return AddrKeys{
		Address: address,
		PubKey:  pubKey,
		PrivKey: privKey,
	}
}

// implement `Interface` in sort package.
type AddrKeysSlice []AddrKeys

func (b AddrKeysSlice) Len() int {
	return len(b)
}

// Sorts lexographically by Address
func (b AddrKeysSlice) Less(i, j int) bool {
	// bytes package already implements Comparable for []byte.
	switch bytes.Compare(b[i].Address.Bytes(), b[j].Address.Bytes()) {
	case -1:
		return true
	case 0, 1:
		return false
	default:
		panic("not fail-able with `bytes.Comparable` bounded [-1, 1].")
	}
}

func (b AddrKeysSlice) Swap(i, j int) {
	b[j], b[i] = b[i], b[j]
}

// CreateGenAccounts generates genesis accounts loaded with coins, and returns
// their addresses, pubkeys, and privkeys.
func CreateGenAccounts(numAccs int, genCoins sdk.Coins) (genAccs []auth.Account,
	addrs []sdk.AccAddress, pubKeys []crypto.PubKey, privKeys []crypto.PrivKey) {

	addrKeysSlice := AddrKeysSlice{}

	for i := 0; i < numAccs; i++ {
		privKey := secp256k1.GenPrivKey()
		pubKey := privKey.PubKey()
		addr := sdk.AccAddress(pubKey.Address())

		addrKeysSlice = append(addrKeysSlice, NewAddrKeys(addr, pubKey, privKey))
	}

	sort.Sort(addrKeysSlice)

	for i := range addrKeysSlice {
		addrs = append(addrs, addrKeysSlice[i].Address)
		pubKeys = append(pubKeys, addrKeysSlice[i].PubKey)
		privKeys = append(privKeys, addrKeysSlice[i].PrivKey)
		genAccs = append(genAccs, &auth.BaseAccount{
			Address: addrKeysSlice[i].Address,
			Coins:   genCoins,
		})
	}

	return
}

// SetGenesis sets the mock app genesis accounts.
func SetGenesis(app *App, accs []auth.Account, notBondedTokens sdk.Int) {
	// Pass the accounts in via the application (lazy) instead of through
	// RequestInitChain.
	app.GenesisAccounts = accs
	app.NotBondedTokens = notBondedTokens

	app.InitChain(abci.RequestInitChain{})
	app.Commit()
}

// GenTx generates a signed mock transaction.
func GenTx(msgs []sdk.Msg, accnums []uint64, seq []uint64, priv ...crypto.PrivKey) auth.StdTx {
	// Make the transaction free
	fee := auth.StdFee{
		Amount: sdk.NewCoins(sdk.NewInt64Coin("foocoin", 0)),
		Gas:    1000000,
	}

	sigs := make([]auth.StdSignature, len(priv))
	memo := "testmemotestmemo"

	for i, p := range priv {
		sig, err := p.Sign(auth.StdSignBytes(chainID, accnums[i], seq[i], fee, msgs, memo))
		if err != nil {
			panic(err)
		}

		sigs[i] = auth.StdSignature{
			PubKey:    p.PubKey(),
			Signature: sig,
		}
	}

	return auth.NewStdTx(msgs, fee, sigs, memo)
}

// GeneratePrivKeys generates a total n secp256k1 private keys.
func GeneratePrivKeys(n int) (keys []crypto.PrivKey) {
	// TODO: Randomize this between ed25519 and secp256k1
	keys = make([]crypto.PrivKey, n)
	for i := 0; i < n; i++ {
		keys[i] = secp256k1.GenPrivKey()
	}

	return
}

// GeneratePrivKeyAddressPairs generates a total of n private key, address
// pairs.
func GeneratePrivKeyAddressPairs(n int) (keys []crypto.PrivKey, addrs []sdk.AccAddress) {
	keys = make([]crypto.PrivKey, n)
	addrs = make([]sdk.AccAddress, n)
	for i := 0; i < n; i++ {
		if rand.Int63()%2 == 0 {
			keys[i] = secp256k1.GenPrivKey()
		} else {
			keys[i] = ed25519.GenPrivKey()
		}
		addrs[i] = sdk.AccAddress(keys[i].PubKey().Address())
	}
	return
}

// GeneratePrivKeyAddressPairsFromRand generates a total of n private key, address
// pairs using the provided randomness source.
func GeneratePrivKeyAddressPairsFromRand(rand *rand.Rand, n int) (keys []crypto.PrivKey, addrs []sdk.AccAddress) {
	keys = make([]crypto.PrivKey, n)
	addrs = make([]sdk.AccAddress, n)
	for i := 0; i < n; i++ {
		secret := make([]byte, 32)
		_, err := rand.Read(secret)
		if err != nil {
			panic("Could not read randomness")
		}
		if rand.Int63()%2 == 0 {
			keys[i] = secp256k1.GenPrivKeySecp256k1(secret)
		} else {
			keys[i] = ed25519.GenPrivKeyFromSecret(secret)
		}
		addrs[i] = sdk.AccAddress(keys[i].PubKey().Address())
	}
	return
}

// RandomSetGenesis set genesis accounts with random coin values using the
// provided addresses and coin denominations.
// nolint: errcheck
func RandomSetGenesis(r *rand.Rand, app *App, addrs []sdk.AccAddress, denoms []string) {
	accts := make([]auth.Account, len(addrs))
	randCoinIntervals := []BigInterval{
		{sdk.NewIntWithDecimal(1, 0), sdk.NewIntWithDecimal(1, 1)},
		{sdk.NewIntWithDecimal(1, 2), sdk.NewIntWithDecimal(1, 3)},
		{sdk.NewIntWithDecimal(1, 40), sdk.NewIntWithDecimal(1, 50)},
	}

	for i := 0; i < len(accts); i++ {
		coins := make([]sdk.Coin, len(denoms))

		// generate a random coin for each denomination
		for j := 0; j < len(denoms); j++ {
			coins[j] = sdk.Coin{Denom: denoms[j],
				Amount: RandFromBigInterval(r, randCoinIntervals),
			}
		}

		app.TotalCoinsSupply = app.TotalCoinsSupply.Add(coins)
		baseAcc := auth.NewBaseAccountWithAddress(addrs[i])

		(&baseAcc).SetCoins(coins)
		accts[i] = &baseAcc
	}
	app.GenesisAccounts = accts
}

// GenSequenceOfTxs generates a set of signed transactions of messages, such
// that they differ only by having the sequence numbers incremented between
// every transaction.
func GenSequenceOfTxs(msgs []sdk.Msg, accnums []uint64, initSeqNums []uint64, numToGenerate int, priv ...crypto.PrivKey) []auth.StdTx {
	txs := make([]auth.StdTx, numToGenerate)
	for i := 0; i < numToGenerate; i++ {
		txs[i] = GenTx(msgs, accnums, initSeqNums, priv...)
		incrementAllSequenceNumbers(initSeqNums)
	}

	return txs
}

func incrementAllSequenceNumbers(initSeqNums []uint64) {
	for i := 0; i < len(initSeqNums); i++ {
		initSeqNums[i]++
	}
}
