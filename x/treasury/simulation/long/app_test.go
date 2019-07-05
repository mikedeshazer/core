// Before running test, recommend to make BlocksPerEpoch small in github.com/terra-projec/core/types/util/epoch.go
package simulation

import (
	"encoding/hex"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/staking"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"

	"github.com/terra-project/core/testutil/mock"
	"github.com/terra-project/core/types/assets"
	"github.com/terra-project/core/types/util"
	"github.com/terra-project/core/x/market"
	"github.com/terra-project/core/x/oracle"
	"github.com/terra-project/core/x/treasury"
)

var (
	priv1 = secp256k1.GenPrivKey()
	addr1 = sdk.AccAddress(priv1.PubKey().Address())
	priv2 = secp256k1.GenPrivKey()
	addr2 = sdk.AccAddress(priv2.PubKey().Address())
	priv3 = secp256k1.GenPrivKey()
	addr3 = sdk.AccAddress(priv3.PubKey().Address())
	priv4 = secp256k1.GenPrivKey()
	addr4 = sdk.AccAddress(priv4.PubKey().Address())

	commissionMsg = staking.NewCommissionMsg(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec())
	denom         = assets.MicroSDRDenom
	rate          = sdk.NewDec(1)
	salt          = "abcd"
)

type Seqs []uint64

// return copy of Seqs object and increase sequence number
func (s *Seqs) inc() Seqs {
	old := append(Seqs{}, (*s)...)
	for i := range *s {
		(*s)[i]++
	}
	return old
}

// set up validators by broadcasting createValidator msg
// make active market prices
func setup(t *testing.T, app *mock.App) Seqs {
	genTokens := sdk.TokensFromTendermintPower(10000000)
	bondTokens := sdk.TokensFromTendermintPower(10)
	genCoin := sdk.NewCoin(assets.MicroLunaDenom, genTokens)
	bondCoin := sdk.NewCoin(assets.MicroLunaDenom, bondTokens)

	// 1 billion sdr
	sdrCoin := sdk.NewCoin(assets.MicroSDRDenom, sdk.NewInt(1000000000*assets.MicroUnit))

	acc1 := &auth.BaseAccount{
		Address: addr1,
		Coins:   sdk.Coins{genCoin, sdrCoin},
	}
	acc2 := &auth.BaseAccount{
		Address: addr2,
		Coins:   sdk.Coins{genCoin},
	}
	acc3 := &auth.BaseAccount{
		Address: addr3,
		Coins:   sdk.Coins{genCoin},
	}
	acc4 := &auth.BaseAccount{
		Address: addr4,
		Coins:   sdk.Coins{genCoin},
	}

	accs := []auth.Account{acc1, acc2, acc3, acc4}

	mock.SetGenesis(app, accs, genCoin.Amount.MulRaw(4))
	mock.CheckBalance(t, app, addr1, sdk.Coins{genCoin, sdrCoin})
	mock.CheckBalance(t, app, addr2, sdk.Coins{genCoin})
	mock.CheckBalance(t, app, addr3, sdk.Coins{genCoin})
	mock.CheckBalance(t, app, addr4, sdk.Coins{genCoin})

	// create validator
	description1 := staking.NewDescription("validator1", "", "", "")
	createValidator1Msg := staking.NewMsgCreateValidator(
		sdk.ValAddress(addr1), priv1.PubKey(), bondCoin, description1, commissionMsg, sdk.OneInt(),
	)

	description2 := staking.NewDescription("validator2", "", "", "")
	createValidator2Msg := staking.NewMsgCreateValidator(
		sdk.ValAddress(addr2), priv2.PubKey(), bondCoin, description2, commissionMsg, sdk.OneInt(),
	)

	description3 := staking.NewDescription("validator3", "", "", "")
	createValidator3Msg := staking.NewMsgCreateValidator(
		sdk.ValAddress(addr3), priv3.PubKey(), bondCoin, description3, commissionMsg, sdk.OneInt(),
	)

	description4 := staking.NewDescription("validator4", "", "", "")
	createValidator4Msg := staking.NewMsgCreateValidator(
		sdk.ValAddress(addr4), priv4.PubKey(), bondCoin, description4, commissionMsg, sdk.OneInt(),
	)

	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{createValidator1Msg, createValidator2Msg, createValidator3Msg, createValidator4Msg},
		[]uint64{0, 1, 2, 3}, []uint64{0, 0, 0, 0}, true, true, []crypto.PrivKey{priv1, priv2, priv3, priv4}...)

	mock.CheckBalance(t, app, addr1, sdk.Coins{genCoin.Sub(bondCoin), sdrCoin})
	mock.CheckBalance(t, app, addr2, sdk.Coins{genCoin.Sub(bondCoin)})
	mock.CheckBalance(t, app, addr3, sdk.Coins{genCoin.Sub(bondCoin)})
	mock.CheckBalance(t, app, addr4, sdk.Coins{genCoin.Sub(bondCoin)})

	return Seqs{1, 1, 1, 1}
}

// generate transactions for given transaction volume
func generateTxForTV(t *testing.T, app *mock.App, seqs Seqs, targetTV sdk.Int) Seqs {
	transferUnit := sdk.NewInt(100 * assets.MicroUnit)
	numOfTx := int(targetTV.Quo(transferUnit).Int64())
	sseqs := seqs[:1]

	sendMsg := bank.NewMsgSend(addr1, addr2, sdk.NewCoins(sdk.NewCoin(assets.MicroSDRDenom, transferUnit)))
	sendMsgs := []sdk.Msg{sendMsg, sendMsg, sendMsg, sendMsg, sendMsg, sendMsg, sendMsg, sendMsg, sendMsg, sendMsg}
	for i := 0; i < numOfTx; i += 10 {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
			sendMsgs[:int(math.Min(10, float64(numOfTx-i)))],
			[]uint64{0}, sseqs.inc(), true, true, []crypto.PrivKey{priv1}...)
	}

	return seqs
}

func makeActiveDenom(t *testing.T, app *mock.App, seqs Seqs) Seqs {
	prevoteMsgs := buildPrevote()
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		prevoteMsgs,
		[]uint64{0, 1, 2, 3}, seqs.inc(), true, true, []crypto.PrivKey{priv1, priv2, priv3, priv4}...)

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})

	oracleParams := app.OracleKeeper.GetParams(ctxCheck)
	for app.LastBlockHeight()%oracleParams.VotePeriod != oracleParams.VotePeriod-1 {
		header = abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, []sdk.Msg{}, []uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	voteMsgs := buildVote()
	header = abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		voteMsgs,
		[]uint64{0, 1, 2, 3}, seqs.inc(), false, true, []crypto.PrivKey{priv1, priv2, priv3, priv4}...)

	for ok := true; ok; ok = (app.LastBlockHeight()%oracleParams.VotePeriod != 0) {
		header = abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, []sdk.Msg{}, []uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	ctxCheck = app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	_, err := app.OracleKeeper.GetLunaSwapRate(ctxCheck, denom)
	require.NoError(t, err)

	return seqs
}

func buildPrevote() []sdk.Msg {
	hashBytes1, _ := oracle.VoteHash(salt, rate, denom, sdk.ValAddress(addr1))
	hashBytes2, _ := oracle.VoteHash(salt, rate, denom, sdk.ValAddress(addr2))
	hashBytes3, _ := oracle.VoteHash(salt, rate, denom, sdk.ValAddress(addr3))
	hashBytes4, _ := oracle.VoteHash(salt, rate, denom, sdk.ValAddress(addr4))

	voteHash1 := hex.EncodeToString(hashBytes1)
	voteHash2 := hex.EncodeToString(hashBytes2)
	voteHash3 := hex.EncodeToString(hashBytes3)
	voteHash4 := hex.EncodeToString(hashBytes4)

	prevoteMsg1 := oracle.NewMsgPricePrevote(voteHash1, denom, addr1, sdk.ValAddress(addr1))
	prevoteMsg2 := oracle.NewMsgPricePrevote(voteHash2, denom, addr2, sdk.ValAddress(addr2))
	prevoteMsg3 := oracle.NewMsgPricePrevote(voteHash3, denom, addr3, sdk.ValAddress(addr3))
	prevoteMsg4 := oracle.NewMsgPricePrevote(voteHash4, denom, addr4, sdk.ValAddress(addr4))

	return []sdk.Msg{prevoteMsg1, prevoteMsg2, prevoteMsg3, prevoteMsg4}
}

func buildVote() []sdk.Msg {

	voteMsg1 := oracle.NewMsgPriceVote(rate, salt, denom, addr1, sdk.ValAddress(addr1))
	voteMsg2 := oracle.NewMsgPriceVote(rate, salt, denom, addr2, sdk.ValAddress(addr2))
	voteMsg3 := oracle.NewMsgPriceVote(rate, salt, denom, addr3, sdk.ValAddress(addr3))
	voteMsg4 := oracle.NewMsgPriceVote(rate, salt, denom, addr4, sdk.ValAddress(addr4))

	return []sdk.Msg{voteMsg1, voteMsg2, voteMsg3, voteMsg4}
}

func swap(t *testing.T, app *mock.App, seqs Seqs, amount sdk.Int) Seqs {
	offerCoin := sdk.NewCoin(assets.MicroLunaDenom, amount)
	swapMsg := market.NewMsgSwap(addr1, offerCoin, denom)

	sseqs := seqs[:1]
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{swapMsg}, []uint64{0}, sseqs.inc(), true, true, []crypto.PrivKey{priv1}...)

	return seqs
}

// Recommended setup for quick test
// BlocksPerEpoch = BlocksPerMinute * 10
func TestTaxRateChange(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)
	seqs = generateTxForTV(t, app, seqs, sdk.NewInt(10000*assets.MicroUnit))

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	params := app.TreasuryKeeper.GetParams(ctxCheck)

	// tax rate increases exactly inc amount
	for i := 0; i < int(params.WindowShort.Int64()); i++ {
		for ok := true; ok; ok = (app.LastBlockHeight()%util.BlocksPerEpoch != 0) {
			header := abci.Header{Height: app.LastBlockHeight() + 1}
			mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
				[]sdk.Msg{},
				[]uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
		}

		oldCtxCheck := ctxCheck.WithBlockHeight(app.LastBlockHeight() - util.BlocksPerEpoch)
		ctxCheck = ctxCheck.WithBlockHeight(app.LastBlockHeight())
		oldTaxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, util.GetEpoch(oldCtxCheck))
		taxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, util.GetEpoch(ctxCheck))

		precision := sdk.NewDecWithPrec(1, 1) // 0.1 is used to fix calculation error
		require.Equal(t, oldTaxRate.Mul(params.MiningIncrement).MulTruncate(precision), taxRate.MulTruncate(precision))
	}

	// Month rolling average will be zero, should increase max amount
	for ok := true; ok; ok = (app.LastBlockHeight()%util.BlocksPerEpoch != 0) {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
			[]sdk.Msg{},
			[]uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	oldCtxCheck := ctxCheck.WithBlockHeight(app.LastBlockHeight() - util.BlocksPerEpoch)
	ctxCheck = ctxCheck.WithBlockHeight(app.LastBlockHeight())
	oldTaxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, util.GetEpoch(oldCtxCheck))
	taxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, util.GetEpoch(ctxCheck))

	require.Equal(t, oldTaxRate.Add(params.TaxPolicy.ChangeRateMax), taxRate)
}

// Recommended setup for quick test
// BlocksPerMinute: 3
// BlocksPerEpoch = BlocksPerDay
func TestRewardWeightChange(t *testing.T) {
	txAmount := sdk.NewInt(10000 * assets.MicroUnit)
	app := mock.NewApp(t)
	seqs := setup(t, app)

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	oracleParams := app.OracleKeeper.GetParams(ctxCheck)
	treasuryParams := app.TreasuryKeeper.GetParams(ctxCheck)

	// Pass first epoch
	for ok := true; ok; ok = (app.LastBlockHeight()%util.BlocksPerEpoch != 0) {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
			[]sdk.Msg{},
			[]uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	// At second epoch, generates same amount of tax and seigniorage
	ctxCheck = ctxCheck.WithBlockHeight(app.LastBlockHeight())
	taxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, util.GetEpoch(ctxCheck))
	oldRewardWeight := app.TreasuryKeeper.GetRewardWeight(ctxCheck, util.GetEpoch(ctxCheck))
	sb := treasuryParams.SeigniorageBurdenTarget
	taxAmount := taxRate.MulInt(txAmount)
	// maks seigniorage to fit seigniorage burden
	// (seigniorage * oldRewardWeight) will be uesd as miner reward
	seigniorageAmount := sb.Quo(sdk.OneDec().Sub(sb)).Mul(taxAmount.Quo(oldRewardWeight))

	fmt.Println(seigniorageAmount)
	seqs = generateTxForTV(t, app, seqs, txAmount)
	seqs = makeActiveDenom(t, app, seqs)
	seqs = swap(t, app, seqs, seigniorageAmount.TruncateInt())

	ctxCheck = ctxCheck.WithBlockHeight(app.LastBlockHeight())
	seigniorage := treasury.SeigniorageRewardsForEpoch(ctxCheck, app.TreasuryKeeper, util.GetEpoch(ctxCheck))
	total := treasury.MiningRewardForEpoch(ctxCheck, app.TreasuryKeeper, util.GetEpoch(ctxCheck))

	for ok := true; ok; ok = (app.LastBlockHeight()%util.BlocksPerEpoch != 0) {
		// before end of epoch period, ensure active denom existence
		height := app.LastBlockHeight()
		if (util.BlocksPerEpoch - height%util.BlocksPerEpoch) == 2*oracleParams.VotePeriod+1 {
			seqs = makeActiveDenom(t, app, seqs)
			continue
		}

		height = app.LastBlockHeight()
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
			[]sdk.Msg{},
			[]uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	ctxCheck = ctxCheck.WithBlockHeight(app.LastBlockHeight())
	rewardWeight := app.TreasuryKeeper.GetRewardWeight(ctxCheck, util.GetEpoch(ctxCheck))

	require.Equal(t, oldRewardWeight.Mul(treasuryParams.SeigniorageBurdenTarget.Quo(seigniorage.Quo(total))), rewardWeight)
}
