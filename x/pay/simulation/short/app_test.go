package simulation

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"

	"github.com/stretchr/testify/require"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"

	"github.com/terra-project/core/testutil/mock"
)

type (
	expectedBalance struct {
		addr  sdk.AccAddress
		coins sdk.Coins
	}

	appTestCase struct {
		expSimPass       bool
		expPass          bool
		msgs             []sdk.Msg
		accNums          []uint64
		accSeqs          []uint64
		privKeys         []crypto.PrivKey
		expectedBalances []expectedBalance
	}
)

var (
	priv1 = secp256k1.GenPrivKey()
	addr1 = sdk.AccAddress(priv1.PubKey().Address())
	priv2 = secp256k1.GenPrivKey()
	addr2 = sdk.AccAddress(priv2.PubKey().Address())
	addr3 = sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	priv4 = secp256k1.GenPrivKey()
	addr4 = sdk.AccAddress(priv4.PubKey().Address())

	firstDenom  = "foocoin"
	secondDenom = "barcoin"

	coins     = sdk.Coins{sdk.NewInt64Coin(firstDenom, 10000000)}
	halfCoins = sdk.Coins{sdk.NewInt64Coin(firstDenom, 5000000)}
	manyCoins = sdk.Coins{sdk.NewInt64Coin(firstDenom, 1000000), sdk.NewInt64Coin(secondDenom, 1000000)}
	freeFee   = auth.NewStdFee(100000, sdk.Coins{sdk.NewInt64Coin(firstDenom, 0)})

	sendMsg1 = bank.NewMsgSend(addr1, addr2, coins)

	multiSendMsg1 = bank.NewMsgMultiSend(
		[]bank.Input{bank.NewInput(addr1, coins)},
		[]bank.Output{bank.NewOutput(addr2, coins)},
	)
	multiSendMsg2 = bank.NewMsgMultiSend(
		[]bank.Input{bank.NewInput(addr1, coins)},
		[]bank.Output{
			bank.NewOutput(addr2, halfCoins),
			bank.NewOutput(addr3, halfCoins),
		},
	)
	multiSendMsg3 = bank.NewMsgMultiSend(
		[]bank.Input{
			bank.NewInput(addr1, coins),
			bank.NewInput(addr4, coins),
		},
		[]bank.Output{
			bank.NewOutput(addr2, coins),
			bank.NewOutput(addr3, coins),
		},
	)
	multiSendMsg4 = bank.NewMsgMultiSend(
		[]bank.Input{
			bank.NewInput(addr2, coins),
		},
		[]bank.Output{
			bank.NewOutput(addr1, coins),
		},
	)
	multiSendMsg5 = bank.NewMsgMultiSend(
		[]bank.Input{
			bank.NewInput(addr1, manyCoins),
		},
		[]bank.Output{
			bank.NewOutput(addr2, manyCoins),
		},
	)
)

func TestSendNotEnoughBalance(t *testing.T) {
	app := mock.NewApp(t)

	// sendAmount > amount; transaction should be fail
	sendAmount := int64(100000000)
	amount := int64(67000000)

	sendCoins := sdk.Coins{sdk.NewInt64Coin(firstDenom, sendAmount)}
	coins := sdk.Coins{sdk.NewInt64Coin(firstDenom, amount)}

	acc := &auth.BaseAccount{
		Address: addr1,
		Coins:   coins,
	}

	mock.SetGenesis(app, []auth.Account{acc}, sdk.NewInt(0))

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{})

	res1 := app.AccountKeeper.GetAccount(ctxCheck, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*auth.BaseAccount))

	origAccNum := res1.GetAccountNumber()
	origSeq := res1.GetSequence()

	sendMsg := bank.NewMsgSend(addr1, addr2, sendCoins)
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, []sdk.Msg{sendMsg}, []uint64{origAccNum}, []uint64{origSeq}, false, false, priv1)

	// The balance should be same,
	mock.CheckBalance(t, app, addr1, coins)

	res2 := app.AccountKeeper.GetAccount(app.NewContext(true, abci.Header{}), addr1)
	require.NotNil(t, res2)

	require.True(t, res2.GetAccountNumber() == origAccNum)
	require.True(t, res2.GetSequence() == origSeq+1)
}

func TestMsgMultiSendWithAccounts(t *testing.T) {
	app := mock.NewApp(t)

	amount := sdk.NewInt(67000000)

	acc := &auth.BaseAccount{
		Address: addr1,
		Coins:   sdk.Coins{sdk.NewCoin(firstDenom, amount)},
	}

	mock.SetGenesis(app, []auth.Account{acc}, sdk.NewInt(0))

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{})

	res1 := app.AccountKeeper.GetAccount(ctxCheck, addr1)
	require.NotNil(t, res1)
	require.Equal(t, acc, res1.(*auth.BaseAccount))

	taxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, sdk.NewInt(0))
	taxCap := app.TreasuryKeeper.GetTaxCap(ctxCheck, firstDenom)

	taxDue := taxRate.MulInt(coins[0].Amount).TruncateInt()
	if taxDue.GT(taxCap) {
		taxDue = taxCap
	}

	testCases := []appTestCase{
		{
			msgs:       []sdk.Msg{multiSendMsg1}, // add1(-coins) | addr2(+coins)
			accNums:    []uint64{0},
			accSeqs:    []uint64{0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []crypto.PrivKey{priv1},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Sub(coins[0].Amount).Sub(taxDue))}},
				{addr2, sdk.Coins{sdk.NewCoin(coins[0].Denom, coins[0].Amount)}},
			},
		},
		{
			msgs:       []sdk.Msg{multiSendMsg1, multiSendMsg2}, // falied due to sequence number
			accNums:    []uint64{0},
			accSeqs:    []uint64{0},
			expSimPass: true, // doesn't check signature
			expPass:    false,
			privKeys:   []crypto.PrivKey{priv1},
		},
	}

	for _, tc := range testCases {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, tc.msgs, tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)

		for _, eb := range tc.expectedBalances {
			mock.CheckBalance(t, app, eb.addr, eb.coins)
		}
	}
}

func TestMsgMultiSendMultipleOut(t *testing.T) {
	app := mock.NewApp(t)

	amount := sdk.NewInt(42000000)
	acc1 := &auth.BaseAccount{
		Address: addr1,
		Coins:   sdk.Coins{sdk.NewCoin(firstDenom, amount)},
	}
	acc2 := &auth.BaseAccount{
		Address: addr2,
		Coins:   sdk.Coins{sdk.NewCoin(firstDenom, amount)},
	}

	mock.SetGenesis(app, []auth.Account{acc1, acc2}, sdk.NewInt(0))

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{})

	taxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, sdk.NewInt(0))
	taxCap := app.TreasuryKeeper.GetTaxCap(ctxCheck, firstDenom)

	taxDue := taxRate.MulInt(coins[0].Amount).TruncateInt()
	if taxDue.GT(taxCap) {
		taxDue = taxCap
	}

	testCases := []appTestCase{
		{
			msgs:       []sdk.Msg{multiSendMsg2}, // addr1(-coins) | addr2(+half),addr3(+half)
			accNums:    []uint64{0},
			accSeqs:    []uint64{0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []crypto.PrivKey{priv1},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Sub(coins[0].Amount).Sub(taxDue))}},
				{addr2, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Add(halfCoins[0].Amount))}},
				{addr3, sdk.Coins{sdk.NewCoin(coins[0].Denom, halfCoins[0].Amount)}},
			},
		},
	}

	for _, tc := range testCases {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, tc.msgs, tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)

		for _, eb := range tc.expectedBalances {
			mock.CheckBalance(t, app, eb.addr, eb.coins)
		}
	}
}

func TestMsgMultiSendMultipleInOut(t *testing.T) {
	app := mock.NewApp(t)

	amount := sdk.NewInt(42000000)
	acc1 := &auth.BaseAccount{
		Address: addr1,
		Coins:   sdk.Coins{sdk.NewCoin(firstDenom, amount)},
	}
	acc2 := &auth.BaseAccount{
		Address: addr2,
		Coins:   sdk.Coins{sdk.NewCoin(firstDenom, amount)},
	}
	acc4 := &auth.BaseAccount{
		Address: addr4,
		Coins:   sdk.Coins{sdk.NewCoin(firstDenom, amount)},
	}

	mock.SetGenesis(app, []auth.Account{acc1, acc2, acc4}, sdk.NewInt(0))

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{})

	taxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, sdk.NewInt(0))
	taxCap := app.TreasuryKeeper.GetTaxCap(ctxCheck, firstDenom)

	taxDue := taxRate.MulInt(coins[0].Amount).TruncateInt()
	if taxDue.GT(taxCap) {
		taxDue = taxCap
	}

	testCases := []appTestCase{
		{
			msgs:       []sdk.Msg{multiSendMsg3}, // addr1(-coins), addr4(-coins) | addr2(+coins), addr3(+coins)
			accNums:    []uint64{0, 2},
			accSeqs:    []uint64{0, 0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []crypto.PrivKey{priv1, priv4},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Sub(coins[0].Amount).Sub(taxDue))}},
				{addr4, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Sub(coins[0].Amount).Sub(taxDue))}},
				{addr2, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Add(coins[0].Amount))}},
				{addr3, sdk.Coins{sdk.NewCoin(coins[0].Denom, coins[0].Amount)}},
			},
		},
	}

	for _, tc := range testCases {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, tc.msgs, tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)

		for _, eb := range tc.expectedBalances {
			mock.CheckBalance(t, app, eb.addr, eb.coins)
		}
	}
}

func TestMsgMultiSendDependent(t *testing.T) {
	app := mock.NewApp(t)

	amount := sdk.NewInt(42000000)
	acc1 := &auth.BaseAccount{
		Address: addr1,
		Coins:   sdk.Coins{sdk.NewCoin(firstDenom, amount)},
	}

	mock.SetGenesis(app, []auth.Account{acc1}, sdk.NewInt(0))

	ctxCheck := app.BaseApp.NewContext(true, abci.Header{})

	taxRate := app.TreasuryKeeper.GetTaxRate(ctxCheck, sdk.NewInt(0))
	taxCap := app.TreasuryKeeper.GetTaxCap(ctxCheck, firstDenom)

	taxDue := taxRate.MulInt(coins[0].Amount).TruncateInt()
	if taxDue.GT(taxCap) {
		taxDue = taxCap
	}

	testCases := []appTestCase{
		{
			msgs:       []sdk.Msg{multiSendMsg1}, // add1(-coins) | addr2(+coins)
			accNums:    []uint64{0},
			accSeqs:    []uint64{0},
			expSimPass: true,
			expPass:    true,
			privKeys:   []crypto.PrivKey{priv1},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Sub(coins[0].Amount).Sub(taxDue))}},
				{addr2, sdk.Coins{sdk.NewCoin(coins[0].Denom, coins[0].Amount)}},
			},
		},
		{
			msgs:       []sdk.Msg{multiSendMsg4}, // add2(-coins) | addr1(+coins)
			accNums:    []uint64{1},
			accSeqs:    []uint64{0},
			expSimPass: false,
			expPass:    false, // fail due to insufficient funds to pay tax
			privKeys:   []crypto.PrivKey{priv2},
			expectedBalances: []expectedBalance{
				{addr1, sdk.Coins{sdk.NewCoin(coins[0].Denom, amount.Sub(coins[0].Amount).Sub(taxDue))}},
			},
		},
	}

	for _, tc := range testCases {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, tc.msgs, tc.accNums, tc.accSeqs, tc.expSimPass, tc.expPass, tc.privKeys...)

		for _, eb := range tc.expectedBalances {
			mock.CheckBalance(t, app, eb.addr, eb.coins)
		}
	}
}
