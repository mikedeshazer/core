package budget

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/terra-project/core/types/assets"
	"github.com/terra-project/core/types/util"
	"github.com/terra-project/core/x/market"
	"github.com/terra-project/core/x/mint"
	"github.com/terra-project/core/x/oracle"
	"github.com/terra-project/core/x/treasury"

	"time"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

var (
	pubKeys = []crypto.PubKey{
		secp256k1.GenPrivKey().PubKey(),
		secp256k1.GenPrivKey().PubKey(),
		secp256k1.GenPrivKey().PubKey(),
	}

	addrs = []sdk.AccAddress{
		sdk.AccAddress(pubKeys[0].Address()),
		sdk.AccAddress(pubKeys[1].Address()),
		sdk.AccAddress(pubKeys[2].Address()),
	}

	valConsPubKeys = []crypto.PubKey{
		ed25519.GenPrivKey().PubKey(),
		ed25519.GenPrivKey().PubKey(),
		ed25519.GenPrivKey().PubKey(),
	}

	valConsAddrs = []sdk.ConsAddress{
		sdk.ConsAddress(valConsPubKeys[0].Address()),
		sdk.ConsAddress(valConsPubKeys[1].Address()),
		sdk.ConsAddress(valConsPubKeys[2].Address()),
	}

	uSDRAmt  = sdk.NewInt(1005 * assets.MicroUnit)
	uLunaAmt = sdk.NewInt(10 * assets.MicroUnit)
)

type testInput struct {
	ctx            sdk.Context
	cdc            *codec.Codec
	mintKeeper     mint.Keeper
	bankKeeper     bank.Keeper
	budgetKeeper   Keeper
	treasuryKeeper TreasuryKeeper
}

func newTestCodec() *codec.Codec {
	cdc := codec.New()

	RegisterCodec(cdc)
	auth.RegisterCodec(cdc)
	sdk.RegisterCodec(cdc)
	codec.RegisterCrypto(cdc)

	return cdc
}

func createTestInput(t *testing.T) testInput {
	keyAcc := sdk.NewKVStoreKey(auth.StoreKey)
	keyParams := sdk.NewKVStoreKey(params.StoreKey)
	tKeyParams := sdk.NewTransientStoreKey(params.TStoreKey)
	keyBudget := sdk.NewKVStoreKey(StoreKey)
	keyMint := sdk.NewKVStoreKey(mint.StoreKey)
	keyStaking := sdk.NewKVStoreKey(staking.StoreKey)
	tKeyStaking := sdk.NewTransientStoreKey(staking.TStoreKey)
	keyTreasury := sdk.NewKVStoreKey(treasury.StoreKey)
	keyMarket := sdk.NewKVStoreKey(market.StoreKey)
	keyOracle := sdk.NewKVStoreKey(oracle.StoreKey)
	keyFeeCollection := sdk.NewKVStoreKey(auth.FeeStoreKey)
	keyDistr := sdk.NewKVStoreKey(distr.StoreKey)
	tKeyDistr := sdk.NewTransientStoreKey(distr.TStoreKey)

	cdc := newTestCodec()
	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ctx := sdk.NewContext(ms, abci.Header{Time: time.Now().UTC()}, false, log.NewNopLogger())

	ms.MountStoreWithDB(keyAcc, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(tKeyParams, sdk.StoreTypeTransient, db)
	ms.MountStoreWithDB(keyParams, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyBudget, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyMint, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyStaking, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(tKeyStaking, sdk.StoreTypeTransient, db)
	ms.MountStoreWithDB(keyTreasury, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyMarket, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyOracle, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(keyDistr, sdk.StoreTypeIAVL, db)
	ms.MountStoreWithDB(tKeyDistr, sdk.StoreTypeTransient, db)
	ms.MountStoreWithDB(keyFeeCollection, sdk.StoreTypeIAVL, db)

	if err := ms.LoadLatestVersion(); err != nil {
		require.Nil(t, err)
	}

	paramsKeeper := params.NewKeeper(cdc, keyParams, tKeyParams)
	accKeeper := auth.NewAccountKeeper(
		cdc,
		keyAcc,
		paramsKeeper.Subspace(auth.DefaultParamspace),
		auth.ProtoBaseAccount,
	)

	bankKeeper := bank.NewBaseKeeper(
		accKeeper,
		paramsKeeper.Subspace(bank.DefaultParamspace),
		bank.DefaultCodespace,
	)

	feeCollectionKeeper := auth.NewFeeCollectionKeeper(
		cdc,
		keyFeeCollection,
	)

	stakingKeeper := staking.NewKeeper(
		cdc,
		keyStaking, tKeyStaking,
		bankKeeper, paramsKeeper.Subspace(staking.DefaultParamspace),
		staking.DefaultCodespace,
	)

	stakingKeeper.SetPool(ctx, staking.InitialPool())
	stakingParams := staking.DefaultParams()
	stakingParams.BondDenom = assets.MicroLunaDenom
	stakingKeeper.SetParams(ctx, stakingParams)

	distrKeeper := distr.NewKeeper(
		cdc, keyDistr, paramsKeeper.Subspace(distr.DefaultParamspace),
		bankKeeper, &stakingKeeper, feeCollectionKeeper, distr.DefaultCodespace,
	)

	mintKeeper := mint.NewKeeper(
		cdc,
		keyMint,
		stakingKeeper,
		bankKeeper,
		accKeeper,
		feeCollectionKeeper,
	)

	oracleKeeper := oracle.NewKeeper(
		cdc,
		keyOracle,
		mintKeeper,
		distrKeeper,
		feeCollectionKeeper,
		stakingKeeper.GetValidatorSet(),
		paramsKeeper.Subspace(oracle.DefaultParamspace),
	)

	marketKeeper := market.NewKeeper(
		cdc,
		keyMarket,
		oracleKeeper,
		mintKeeper,
		paramsKeeper.Subspace(market.DefaultParamspace),
	)

	treasuryKeeper := treasury.NewKeeper(
		cdc,
		keyTreasury,
		stakingKeeper.GetValidatorSet(),
		mintKeeper,
		marketKeeper,
		paramsKeeper.Subspace(treasury.DefaultParamspace),
	)

	sh := staking.NewHandler(stakingKeeper)
	for i, addr := range addrs {
		err := mintKeeper.Mint(ctx, addr, sdk.NewCoin(assets.MicroSDRDenom, uSDRAmt))
		err2 := mintKeeper.Mint(ctx, addr, sdk.NewCoin(assets.MicroLunaDenom, uLunaAmt))

		require.NoError(t, err)
		require.NoError(t, err2)

		// Add validators
		commission := staking.NewCommissionMsg(sdk.NewDecWithPrec(5, 1), sdk.NewDecWithPrec(5, 1), sdk.NewDec(0))
		msg := staking.NewMsgCreateValidator(sdk.ValAddress(addr), valConsPubKeys[i],
			sdk.NewCoin(assets.MicroLunaDenom, uLunaAmt), staking.Description{}, commission, sdk.OneInt())
		res := sh(ctx, msg)
		require.True(t, res.IsOK())

		distrKeeper.Hooks().AfterValidatorCreated(ctx, sdk.ValAddress(addr))
		staking.EndBlocker(ctx, stakingKeeper)
	}

	budgetKeeper := NewKeeper(
		cdc,
		keyBudget,
		marketKeeper,
		mintKeeper,
		treasuryKeeper,
		stakingKeeper.GetValidatorSet(),
		paramsKeeper.Subspace(DefaultParamspace),
	)

	InitGenesis(ctx, budgetKeeper, DefaultGenesisState())

	return testInput{ctx, cdc, mintKeeper, bankKeeper, budgetKeeper, treasuryKeeper}
}

func generateTestProgram(ctx sdk.Context, budgetKeeper Keeper, accounts ...sdk.AccAddress) Program {
	submitter := addrs[0]
	if len(accounts) > 0 {
		submitter = accounts[0]
	}

	executor := addrs[1]
	if len(accounts) > 1 {
		executor = accounts[1]
	}

	testProgramID := budgetKeeper.NewProgramID(ctx)

	return NewProgram(testProgramID, "testTitle", "testDescription", submitter, executor, util.GetEpoch(ctx).Int64())
}
