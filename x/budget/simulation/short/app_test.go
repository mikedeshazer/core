package simulation

import (
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
	"github.com/terra-project/core/x/budget"
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

	programID = budget.InitialProgramID
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
		Coins:   sdk.Coins{genCoin, budget.DefaultParams().Deposit},
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
	mock.CheckBalance(t, app, addr1, sdk.Coins{genCoin, budget.DefaultParams().Deposit})
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

	mock.CheckBalance(t, app, addr1, sdk.Coins{genCoin.Sub(bondCoin), budget.DefaultParams().Deposit})
	mock.CheckBalance(t, app, addr2, sdk.Coins{genCoin.Sub(bondCoin)})
	mock.CheckBalance(t, app, addr3, sdk.Coins{genCoin.Sub(bondCoin)})
	mock.CheckBalance(t, app, addr4, sdk.Coins{genCoin.Sub(bondCoin)})

	return Seqs{1, 1, 1, 1}
}

func registerProgram(t *testing.T, app *mock.App, seqs Seqs) Seqs {
	submitProgramMsg := budget.NewMsgSubmitProgram("title", "description", addr1, addr1)

	sseqs := seqs[:1]
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{submitProgramMsg},
		[]uint64{0}, sseqs.inc(), true, true, []crypto.PrivKey{priv1}...)

	checkCtx := app.BaseApp.NewContext(true, header)
	budgetParams := app.BudgetKeeper.GetParams(checkCtx)

	program, err := app.BudgetKeeper.GetProgram(checkCtx, programID)
	require.NoError(t, err)
	require.NotNil(t, program)

	endBlock := program.SubmitBlock + budgetParams.VotePeriod
	isCandidate := app.BudgetKeeper.CandQueueHas(checkCtx, endBlock, programID)
	require.True(t, isCandidate)

	return seqs
}

func withdrawProgram(t *testing.T, app *mock.App, seqs Seqs) Seqs {

	// program should exists
	checkCtx := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	program, err := app.BudgetKeeper.GetProgram(checkCtx, programID)
	require.NoError(t, err)
	require.NotNil(t, program)

	withdrawProgramMsg := budget.NewMsgWithdrawProgram(programID, addr1)
	sseqs := seqs[:1]
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{withdrawProgramMsg},
		[]uint64{0}, sseqs.inc(), true, true, []crypto.PrivKey{priv1}...)

	checkCtx = app.BaseApp.NewContext(true, header)
	budgetParams := app.BudgetKeeper.GetParams(checkCtx)

	_, err = app.BudgetKeeper.GetProgram(checkCtx, programID)
	require.Error(t, err)

	endBlock := program.SubmitBlock + budgetParams.VotePeriod
	isCandidate := app.BudgetKeeper.CandQueueHas(checkCtx, endBlock, programID)
	require.False(t, isCandidate)

	return seqs
}

func vote(t *testing.T, app *mock.App, seqs Seqs, option bool) Seqs {
	header := abci.Header{Height: app.LastBlockHeight() + 1}
	mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header,
		[]sdk.Msg{
			budget.NewMsgVoteProgram(programID, option, addr1),
			budget.NewMsgVoteProgram(programID, option, addr2),
			budget.NewMsgVoteProgram(programID, option, addr3),
			budget.NewMsgVoteProgram(programID, option, addr4),
		},
		[]uint64{0, 1, 2, 3}, seqs.inc(), true, true, []crypto.PrivKey{priv1, priv2, priv3, priv4}...)

	checkCtx := app.BaseApp.NewContext(true, header)
	budgetParams := app.BudgetKeeper.GetParams(checkCtx)

	for i := 0; i < int(budgetParams.VotePeriod); i++ {
		header = abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, []sdk.Msg{}, []uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	return seqs
}

func TestProgramRegister(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)
	seqs = registerProgram(t, app, seqs)
	seqs = vote(t, app, seqs, true)

	// Program should be active
	checkCtx := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	budgetParams := app.BudgetKeeper.GetParams(checkCtx)

	program, err := app.BudgetKeeper.GetProgram(checkCtx, programID)
	require.NoError(t, err)
	require.NotNil(t, program)

	endBlock := program.SubmitBlock + budgetParams.VotePeriod
	isCandidate := app.BudgetKeeper.CandQueueHas(checkCtx, endBlock, programID)
	require.False(t, isCandidate)

	// If validators do not change their intention, program should be left active
	for i := 0; i < int(budgetParams.VotePeriod)*5; i++ {
		header := abci.Header{Height: app.LastBlockHeight() + 1}
		mock.SignCheckDeliver(t, app.Cdc, app.BaseApp, header, []sdk.Msg{}, []uint64{}, []uint64{}, true, true, []crypto.PrivKey{}...)
	}

	checkCtx = app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})

	program, err = app.BudgetKeeper.GetProgram(checkCtx, programID)
	require.NoError(t, err)
	require.NotNil(t, program)

	isCandidate = app.BudgetKeeper.CandQueueHas(checkCtx, endBlock, programID)
	require.False(t, isCandidate)

}

func TestProgramWithdraw(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)
	seqs = registerProgram(t, app, seqs)
	seqs = withdrawProgram(t, app, seqs)
}

func TestVoteThreshold(t *testing.T) {
	app := mock.NewApp(t)
	seqs := setup(t, app)
	seqs = registerProgram(t, app, seqs)
	seqs = vote(t, app, seqs, true)

	// Program should be active
	checkCtx := app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	budgetParams := app.BudgetKeeper.GetParams(checkCtx)

	program, err := app.BudgetKeeper.GetProgram(checkCtx, programID)
	require.NoError(t, err)
	require.NotNil(t, program)

	endBlock := program.SubmitBlock + budgetParams.VotePeriod
	isCandidate := app.BudgetKeeper.CandQueueHas(checkCtx, endBlock, programID)
	require.False(t, isCandidate)

	// validators change their option
	seqs = vote(t, app, seqs, false)

	// Program should be deleted
	checkCtx = app.BaseApp.NewContext(true, abci.Header{Height: app.LastBlockHeight()})
	_, err = app.BudgetKeeper.GetProgram(checkCtx, programID)
	require.Error(t, err)
}
