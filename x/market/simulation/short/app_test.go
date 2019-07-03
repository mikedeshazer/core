package simulation

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/staking"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"

	"github.com/terra-project/core/testutil/mock"
	"github.com/terra-project/core/types/assets"
	"github.com/terra-project/core/x/market"
	"github.com/terra-project/core/x/oracle"
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
	denom         = "foocoin"
	rate          = sdk.NewDec(8712)
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
	genTokens := sdk.TokensFromTendermintPower(100)
	bondTokens := sdk.TokensFromTendermintPower(10)
	genCoin := sdk.NewCoin(assets.MicroLunaDenom, genTokens)
	bondCoin := sdk.NewCoin(assets.MicroLunaDenom, bondTokens)

	acc1 := &auth.BaseAccount{
		Address: addr1,
		Coins:   sdk.Coins{genCoin},
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
	mock.CheckBalance(t, app, addr1, sdk.Coins{genCoin})
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

	mock.CheckBalance(t, app, addr1, sdk.Coins{genCoin.Sub(bondCoin)})
	mock.CheckBalance(t, app, addr2, sdk.Coins{genCoin.Sub(bondCoin)})
	mock.CheckBalance(t, app, addr3, sdk.Coins{genCoin.Sub(bondCoin)})
	mock.CheckBalance(t, app, addr4, sdk.Coins{genCoin.Sub(bondCoin)})

	return Seqs{1, 1, 1, 1}
}

func makeActiveDenom(t *testing.T, app *mock.App, seqs Seqs) Seqs {
	prevoteMsgs := buildPrevote()
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		prevoteMsgs,
		[]uint64{0, 1, 2, 3}, seqs.inc(), true, true, []crypto.PrivKey{priv1, priv2, priv3, priv4}...)

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})

	oracleParams := app.OracleKeeper.GetParams(ctxCheck)
	for i := 0; i < int(oracleParams.VotePeriod); i++ {
		header = abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, []sdk.Msg{}, []uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	voteMsgs := buildVote()
	header = abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		voteMsgs,
		[]uint64{0, 1, 2, 3}, seqs.inc(), true, true, []crypto.PrivKey{priv1, priv2, priv3, priv4}...)

	for i := 0; i < int(oracleParams.VotePeriod); i++ {
		header = abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, []sdk.Msg{}, []uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	ctxCheck = app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	_, err := app.OracleKeeper.GetLunaSwapRate(ctxCheck, denom)
	require.NoError(t, err)

	return seqs
}

func buildPrevote() []sdk.Msg {
	salt := "abcd"
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
	salt := "abcd"

	voteMsg1 := oracle.NewMsgPriceVote(rate, salt, denom, addr1, sdk.ValAddress(addr1))
	voteMsg2 := oracle.NewMsgPriceVote(rate, salt, denom, addr2, sdk.ValAddress(addr2))
	voteMsg3 := oracle.NewMsgPriceVote(rate, salt, denom, addr3, sdk.ValAddress(addr3))
	voteMsg4 := oracle.NewMsgPriceVote(rate, salt, denom, addr4, sdk.ValAddress(addr4))

	return []sdk.Msg{voteMsg1, voteMsg2, voteMsg3, voteMsg4}
}

func TestNormalSwapAndIssuanceChange(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)
	seqs = makeActiveDenom(t, app, seqs)

	offerAmount := sdk.TokensFromTendermintPower(1)
	offerCoin := sdk.NewCoin(assets.MicroLunaDenom, offerAmount)
	swapMsg := market.NewMsgSwap(addr1, offerCoin, denom)

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	oldIssuance := app.MintKeeper.GetIssuance(ctxCheck, assets.MicroLunaDenom, sdk.ZeroInt())

	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{swapMsg}, []uint64{0}, []uint64{seqs[0]}, true, true, []crypto.PrivKey{priv1}...)
	seqs[0]++

	ctxCheck = app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	curIssuance := app.MintKeeper.GetIssuance(ctxCheck, assets.MicroLunaDenom, sdk.ZeroInt())

	require.Equal(t, oldIssuance.Sub(offerAmount), curIssuance)
}

func TestUnregisteredDenomSwap(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)

	offerAmount := sdk.TokensFromTendermintPower(1)
	offerCoin := sdk.NewCoin(assets.MicroLunaDenom, offerAmount)
	swapMsg := market.NewMsgSwap(addr1, offerCoin, denom)
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{swapMsg}, []uint64{0}, []uint64{seqs[0]}, false, false, []crypto.PrivKey{priv1}...)
	seqs[0]++

	seqs = makeActiveDenom(t, app, seqs)
	header = abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{swapMsg}, []uint64{0}, []uint64{seqs[0]}, true, true, []crypto.PrivKey{priv1}...)
	seqs[0]++
}

func TestRecursiveSwap(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)
	seqs = makeActiveDenom(t, app, seqs)

	offerAmount := sdk.TokensFromTendermintPower(1)
	offerCoin := sdk.NewCoin(assets.MicroLunaDenom, offerAmount)
	swapMsg := market.NewMsgSwap(addr1, offerCoin, assets.MicroLunaDenom)
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{swapMsg}, []uint64{0}, []uint64{seqs[0]}, false, false, []crypto.PrivKey{priv1}...)
	seqs[0]++
}

func TestInsufficientSwap(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)
	seqs = makeActiveDenom(t, app, seqs)

	offerAmount := sdk.TokensFromTendermintPower(100)
	offerCoin := sdk.NewCoin(assets.MicroLunaDenom, offerAmount)
	swapMsg := market.NewMsgSwap(addr1, offerCoin, denom)
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{swapMsg}, []uint64{0}, []uint64{seqs[0]}, false, false, []crypto.PrivKey{priv1}...)
	seqs[0]++
}
