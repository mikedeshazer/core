package app

import (
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/crisis"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/x/auth"
	distr "github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	"github.com/cosmos/cosmos-sdk/x/staking"

	"github.com/terra-project/core/x/budget"
	"github.com/terra-project/core/x/market"
	"github.com/terra-project/core/x/oracle"
	"github.com/terra-project/core/x/treasury"

	abci "github.com/tendermint/tendermint/abci/types"
)

func setGenesis(t *testing.T, tapp *TerraApp, accs ...*auth.BaseAccount) error {
	genaccs := make([]GenesisAccount, len(accs))
	for i, acc := range accs {
		genaccs[i] = NewGenesisAccount(acc)
	}

	genesisState := NewGenesisState(
		genaccs,
		auth.DefaultGenesisState(),
		bank.DefaultGenesisState(),
		staking.DefaultGenesisState(),
		distr.DefaultGenesisState(),
		oracle.DefaultGenesisState(),
		budget.DefaultGenesisState(),
		crisis.DefaultGenesisState(),
		treasury.DefaultGenesisState(),
		slashing.DefaultGenesisState(),
		market.DefaultGenesisState(),
	)

	stateBytes, err := codec.MarshalJSONIndent(tapp.cdc, genesisState)
	if err != nil {
		return err
	}

	emptyCommitID := sdk.CommitID{}
	lastID := tapp.LastCommitID()
	require.Equal(t, emptyCommitID, lastID)
	// Initialize the chain
	vals := []abci.ValidatorUpdate{}
	tapp.InitChain(abci.RequestInitChain{Validators: vals, AppStateBytes: stateBytes})
	tapp.Commit()

	// fresh store has zero/empty last commit
	lastHeight := tapp.LastBlockHeight()
	require.Equal(t, int64(1), lastHeight)

	require.NotPanics(t, func() {
		// execute a block, collect commit ID
		header := abci.Header{Height: 2}
		tapp.BeginBlock(abci.RequestBeginBlock{Header: header})
		tapp.EndBlock(abci.RequestEndBlock{})
		tapp.Commit()
	})

	return nil
}

func TestTerradExport(t *testing.T) {
	db := db.NewMemDB()
	tapp := NewTerraApp(log.NewTMLogger(log.NewSyncWriter(os.Stdout)), db, nil, true, false)
	setGenesis(t, tapp)

	// Making a new app object with the db, so that initchain hasn't been called
	newTapp := NewTerraApp(log.NewTMLogger(log.NewSyncWriter(os.Stdout)), db, nil, true, false)
	_, _, err := newTapp.ExportAppStateAndValidators(true, []string{})
	require.NoError(t, err, "ExportAppStateAndValidators should not have an error for zero height")

	_, _, err = newTapp.ExportAppStateAndValidators(false, []string{})
	require.NoError(t, err, "ExportAppStateAndValidators should not have an error")
}
