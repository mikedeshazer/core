package auth

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/multisig"
	"github.com/tendermint/tendermint/crypto/secp256k1"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"

	"github.com/terra-project/core/types/assets"
	"github.com/terra-project/core/types/util"
)

var (
	// simulation signature values used to estimate gas consumption
	simSecp256k1Pubkey secp256k1.PubKeySecp256k1
	simSecp256k1Sig    [64]byte
	msgCdc             = codec.New()
)

func init() {
	// This decodes a valid hex string into a sepc256k1Pubkey for use in transaction simulation
	bz, _ := hex.DecodeString("035AD6810A47F073553FF30D2FCC7E0D3B1C0B74B61A1AAA2582344037151E143A")
	copy(simSecp256k1Pubkey[:], bz)

	auth.RegisterCodec(msgCdc)
	codec.RegisterCrypto(msgCdc)
}

// NewAnteHandler returns an AnteHandler that checks and increments sequence
// numbers, checks signatures & account numbers, and deducts fees from the first
// signer.
func NewAnteHandler(ak auth.AccountKeeper, fck auth.FeeCollectionKeeper, tk TreasuryKeeper) sdk.AnteHandler {
	return func(
		ctx sdk.Context, tx sdk.Tx, simulate bool,
	) (newCtx sdk.Context, res sdk.Result, abort bool) {

		// all transactions must be of type auth.StdTx
		stdTx, ok := tx.(auth.StdTx)
		if !ok {
			// Set a gas meter with limit 0 as to prevent an infinite gas meter attack
			// during runTx.
			newCtx = SetGasMeter(simulate, ctx, 0)
			return newCtx, sdk.ErrInternal("tx must be StdTx").Result(), true
		}

		params := ak.GetParams(ctx)
		taxes := filterMsgAndComputeTax(ctx, tk, stdTx.GetMsgs())

		// Ensure given fee is enough to cover taxes
		if !simulate {
			if _, hasNeg := stdTx.Fee.Amount.SafeSub(taxes); hasNeg {
				return newCtx, sdk.ErrInsufficientFee(
					fmt.Sprintf("insufficient fees to pay for taxes; %s < %s", stdTx.Fee.Amount, taxes),
				).Result(), true
			}
		}

		// Ensure that the provided fees meet a minimum threshold for the validator,
		// if this is a CheckTx. This is only for local mempool purposes, and thus
		// is only ran on check tx.
		if ctx.IsCheckTx() && !simulate {
			res := EnsureSufficientMempoolFees(ctx, stdTx.Fee, taxes)
			if !res.IsOK() {
				return newCtx, res, true
			}
		}

		newCtx = SetGasMeter(simulate, ctx, stdTx.Fee.Gas)

		// AnteHandlers must have their own defer/recover in order for the BaseApp
		// to know how much gas was used! This is because the GasMeter is created in
		// the AnteHandler, but if it panics the context won't be set properly in
		// runTx's recover call.
		defer func() {
			if r := recover(); r != nil {
				switch rType := r.(type) {
				case sdk.ErrorOutOfGas:
					log := fmt.Sprintf(
						"out of gas in location: %v; gasWanted: %d, gasUsed: %d",
						rType.Descriptor, stdTx.Fee.Gas, newCtx.GasMeter().GasConsumed(),
					)
					res = sdk.ErrOutOfGas(log).Result()

					res.GasWanted = stdTx.Fee.Gas
					res.GasUsed = newCtx.GasMeter().GasConsumed()
					abort = true
				default:
					panic(r)
				}
			}
		}()

		if err := tx.ValidateBasic(); err != nil {
			return newCtx, err.Result(), true
		}

		newCtx.GasMeter().ConsumeGas(params.TxSizeCostPerByte*sdk.Gas(len(newCtx.TxBytes())), "txSize")

		if res := ValidateMemo(stdTx, params); !res.IsOK() {
			return newCtx, res, true
		}

		if res := ValidateSigCount(stdTx, params); !res.IsOK() {
			return newCtx, res, true
		}

		// stdSigs contains the sequence number, account number, and signatures.
		// When simulating, this would just be a 0-length slice.
		signerAddrs := stdTx.GetSigners()
		signerAccs := make([]auth.Account, len(signerAddrs))
		isGenesis := ctx.BlockHeight() == 0

		// fetch first signer, who's going to pay the fees
		signerAccs[0], res = GetSignerAcc(newCtx, ak, signerAddrs[0])
		if !res.IsOK() {
			return newCtx, res, true
		}

		if !stdTx.Fee.Amount.IsZero() {
			signerAccs[0], res = DeductFees(ctx.BlockHeader().Time, signerAccs[0], stdTx.Fee)
			if !res.IsOK() {
				return newCtx, res, true
			}

			fck.AddCollectedFees(newCtx, stdTx.Fee.Amount)
			tk.RecordTaxProceeds(ctx, stdTx.Fee.Amount)
		}

		// stdSigs contains the sequence number, account number, and signatures.
		// When simulating, this would just be a 0-length slice.
		stdSigs := stdTx.GetSignatures()

		for i := 0; i < len(stdSigs); i++ {
			// skip the fee payer, account is cached and fees were deducted already
			if i != 0 {
				signerAccs[i], res = GetSignerAcc(newCtx, ak, signerAddrs[i])
				if !res.IsOK() {
					return newCtx, res, true
				}
			}

			// check signature, return account with incremented nonce
			signBytes := GetSignBytes(newCtx.ChainID(), stdTx, signerAccs[i], isGenesis)
			signerAccs[i], res = processSig(newCtx, signerAccs[i], stdSigs[i], signBytes, simulate, params)
			if !res.IsOK() {
				return newCtx, res, true
			}

			ak.SetAccount(newCtx, signerAccs[i])
		}

		// TODO: tx tags (?)
		return newCtx, sdk.Result{GasWanted: stdTx.Fee.Gas}, false // continue...
	}
}

// GetSignerAcc returns an account for a given address that is expected to sign
// a transaction.
func GetSignerAcc(ctx sdk.Context, ak auth.AccountKeeper, addr sdk.AccAddress) (auth.Account, sdk.Result) {
	if acc := ak.GetAccount(ctx, addr); acc != nil {
		return acc, sdk.Result{}
	}
	return nil, sdk.ErrUnknownAddress(fmt.Sprintf("account %s does not exist", addr)).Result()
}

// ValidateMemo validates the memo size.
func ValidateMemo(stdTx auth.StdTx, params auth.Params) sdk.Result {
	memoLength := len(stdTx.GetMemo())
	if uint64(memoLength) > params.MaxMemoCharacters {
		return sdk.ErrMemoTooLarge(
			fmt.Sprintf(
				"maximum number of characters is %d but received %d characters",
				params.MaxMemoCharacters, memoLength,
			),
		).Result()
	}

	return sdk.Result{}
}

// ValidateSigCount validates the signature counts.
func ValidateSigCount(stdTx auth.StdTx, params auth.Params) sdk.Result {
	stdSigs := stdTx.GetSignatures()
	sigCount := 0
	for i := 0; i < len(stdSigs); i++ {
		sigCount += countSubKeys(stdSigs[i].PubKey)
		if uint64(sigCount) > params.TxSigLimit {
			return sdk.ErrTooManySignatures(
				fmt.Sprintf("signatures: %d, limit: %d", sigCount, params.TxSigLimit),
			).Result()
		}
	}

	return sdk.Result{}
}

// verify the signature and increment the sequence. If the account doesn't have
// a pubkey, set it.
func processSig(
	ctx sdk.Context, acc auth.Account, sig auth.StdSignature, signBytes []byte, simulate bool, params auth.Params,
) (updatedAcc auth.Account, res sdk.Result) {

	pubKey, res := ProcessPubKey(acc, sig, simulate)
	if !res.IsOK() {
		return nil, res
	}

	err := acc.SetPubKey(pubKey)
	if err != nil {
		return nil, sdk.ErrInternal("setting PubKey on signer's account").Result()
	}

	if simulate {
		// Simulated txs should not contain a signature and are not required to
		// contain a pubkey, so we must account for tx size of including a
		// StdSignature (Amino encoding) and simulate gas consumption
		// (assuming a SECP256k1 simulation key).
		consumeSimSigGas(ctx.GasMeter(), pubKey, sig, params)
	}

	if res := consumeSigVerificationGas(ctx.GasMeter(), sig.Signature, pubKey, params); !res.IsOK() {
		return nil, res
	}

	if !simulate && !pubKey.VerifyBytes(signBytes, sig.Signature) {
		return nil, sdk.ErrUnauthorized("signature verification failed; verify correct account sequence and chain-id").Result()
	}

	if err := acc.SetSequence(acc.GetSequence() + 1); err != nil {
		panic(err)
	}

	return acc, res
}

// countSubKeys counts the total number of keys for a multi-sig public key.
func countSubKeys(pub crypto.PubKey) int {
	v, ok := pub.(multisig.PubKeyMultisigThreshold)
	if !ok {
		return 1
	}

	numKeys := 0
	for _, subkey := range v.PubKeys {
		numKeys += countSubKeys(subkey)
	}

	return numKeys
}

func consumeSimSigGas(gasmeter sdk.GasMeter, pubkey crypto.PubKey, sig auth.StdSignature, params auth.Params) {
	simSig := auth.StdSignature{PubKey: pubkey}
	if len(sig.Signature) == 0 {
		simSig.Signature = simSecp256k1Sig[:]
	}

	sigBz := msgCdc.MustMarshalBinaryLengthPrefixed(simSig)
	cost := sdk.Gas(len(sigBz) + 6)

	// If the pubkey is a multi-signature pubkey, then we estimate for the maximum
	// number of signers.
	if _, ok := pubkey.(multisig.PubKeyMultisigThreshold); ok {
		cost *= params.TxSigLimit
	}

	gasmeter.ConsumeGas(params.TxSizeCostPerByte*cost, "txSize")
}

// ProcessPubKey verifies that the given account address matches that of the
// StdSignature. In addition, it will set the public key of the account if it
// has not been set.
func ProcessPubKey(acc auth.Account, sig auth.StdSignature, simulate bool) (crypto.PubKey, sdk.Result) {
	// If pubkey is not known for account, set it from the StdSignature.
	pubKey := acc.GetPubKey()
	if simulate {
		// In simulate mode the transaction comes with no signatures, thus if the
		// account's pubkey is nil, both signature verification and gasKVStore.Set()
		// shall consume the largest amount, i.e. it takes more gas to verify
		// secp256k1 keys than ed25519 ones.
		if pubKey == nil {
			return simSecp256k1Pubkey, sdk.Result{}
		}

		return pubKey, sdk.Result{}
	}

	if pubKey == nil {
		pubKey = sig.PubKey
		if pubKey == nil {
			return nil, sdk.ErrInvalidPubKey("PubKey not found").Result()
		}

		if !bytes.Equal(pubKey.Address(), acc.GetAddress()) {
			return nil, sdk.ErrInvalidPubKey(
				fmt.Sprintf("PubKey does not match Signer address %s", acc.GetAddress())).Result()
		}
	}

	return pubKey, sdk.Result{}
}

// consumeSigVerificationGas consumes gas for signature verification based upon
// the public key type. The cost is fetched from the given params and is matched
// by the concrete type.
//
// TODO: Design a cleaner and flexible way to match concrete public key types.
func consumeSigVerificationGas(
	meter sdk.GasMeter, sig []byte, pubkey crypto.PubKey, params auth.Params,
) sdk.Result {

	pubkeyType := strings.ToLower(fmt.Sprintf("%T", pubkey))

	switch {
	case strings.Contains(pubkeyType, "ed25519"):
		meter.ConsumeGas(params.SigVerifyCostED25519, "ante verify: ed25519")
		return sdk.ErrInvalidPubKey("ED25519 public keys are unsupported").Result()

	case strings.Contains(pubkeyType, "secp256k1"):
		meter.ConsumeGas(params.SigVerifyCostSecp256k1, "ante verify: secp256k1")
		return sdk.Result{}

	case strings.Contains(pubkeyType, "multisigthreshold"):
		var multisignature multisig.Multisignature
		codec.Cdc.MustUnmarshalBinaryBare(sig, &multisignature)

		multisigPubKey := pubkey.(multisig.PubKeyMultisigThreshold)
		consumeMultisignatureVerificationGas(meter, multisignature, multisigPubKey, params)
		return sdk.Result{}

	default:
		return sdk.ErrInvalidPubKey(fmt.Sprintf("unrecognized public key type: %s", pubkeyType)).Result()
	}
}

func consumeMultisignatureVerificationGas(meter sdk.GasMeter,
	sig multisig.Multisignature, pubkey multisig.PubKeyMultisigThreshold,
	params auth.Params) {

	size := sig.BitArray.Size()
	sigIndex := 0
	for i := 0; i < size; i++ {
		if sig.BitArray.GetIndex(i) {
			consumeSigVerificationGas(meter, sig.Sigs[sigIndex], pubkey.PubKeys[i], params)
			sigIndex++
		}
	}
}

// DeductFees deducts fees from the given account.
//
// NOTE: We could use the CoinKeeper (in addition to the AccountKeeper, because
// the CoinKeeper doesn't give us accounts), but it seems easier to do this.
func DeductFees(blockTime time.Time, acc auth.Account, fee auth.StdFee) (auth.Account, sdk.Result) {
	coins := acc.GetCoins()
	feeAmount := fee.Amount

	if !feeAmount.IsValid() {
		return nil, sdk.ErrInsufficientFee(fmt.Sprintf("invalid fee amount: %s", feeAmount)).Result()
	}

	// get the resulting coins deducting the fees
	newCoins, hasNeg := coins.SafeSub(feeAmount)
	if hasNeg {
		return nil, sdk.ErrInsufficientFunds(
			fmt.Sprintf("insufficient funds to pay for fees; %s < %s", coins, feeAmount),
		).Result()
	}

	// Validate the account has enough "spendable" coins as this will cover cases
	// such as vesting accounts.
	spendableCoins := acc.SpendableCoins(blockTime)
	if _, hasNeg := spendableCoins.SafeSub(feeAmount); hasNeg {
		return nil, sdk.ErrInsufficientFunds(
			fmt.Sprintf("insufficient funds to pay for fees; %s < %s", spendableCoins, feeAmount),
		).Result()
	}

	if err := acc.SetCoins(newCoins); err != nil {
		return nil, sdk.ErrInternal(err.Error()).Result()
	}

	return acc, sdk.Result{}
}

// filterMsgAndComputeTax computes the stability tax on MsgSend and MsgMultiSend.
func filterMsgAndComputeTax(ctx sdk.Context, tk TreasuryKeeper, msgs []sdk.Msg) (taxes sdk.Coins) {
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case bank.MsgSend:
			taxes = taxes.Add(computeTax(ctx, tk, msg.Amount))

		case bank.MsgMultiSend:
			for _, input := range msg.Inputs {
				taxes = taxes.Add(computeTax(ctx, tk, input.Coins))
			}
		}
	}

	return
}

// computes the stability tax according to tax-rate and tax-cap
func computeTax(ctx sdk.Context, tk TreasuryKeeper, principal sdk.Coins) (taxes sdk.Coins) {
	taxRate := tk.GetTaxRate(ctx, util.GetEpoch(ctx))
	if taxRate.Equal(sdk.ZeroDec()) {
		return
	}

	for _, coin := range principal {
		if coin.Denom == assets.MicroLunaDenom {
			continue
		}

		taxDue := sdk.NewDecFromInt(coin.Amount).Mul(taxRate).TruncateInt()

		// If tax due is greater than the tax cap, cap!
		taxCap := tk.GetTaxCap(ctx, coin.Denom)
		if taxDue.GT(taxCap) {
			taxDue = taxCap
		}

		if taxDue.Equal(sdk.ZeroInt()) {
			continue
		}

		taxes = taxes.Add(sdk.NewCoins(sdk.NewCoin(coin.Denom, taxDue)))
	}

	return
}

// EnsureSufficientMempoolFees verifies that the given transaction has supplied
// enough fees(gas + stability) to cover a proposer's minimum fees. A result object is returned
// indicating success or failure.
//
// Contract: This should only be called during CheckTx as it cannot be part of
// consensus.
func EnsureSufficientMempoolFees(ctx sdk.Context, stdFee auth.StdFee, taxes sdk.Coins) sdk.Result {

	var requiredFees sdk.Coins
	minGasPrices := ctx.MinGasPrices()
	if !minGasPrices.IsZero() {
		requiredFees = make(sdk.Coins, len(minGasPrices))

		// Determine the required fees by multiplying each required minimum gas
		// price by the gas limit, where fee = ceil(minGasPrice * gasLimit).
		glDec := sdk.NewDec(int64(stdFee.Gas))
		for i, gp := range minGasPrices {
			fee := gp.Amount.Mul(glDec)
			requiredFees[i] = sdk.NewCoin(gp.Denom, fee.Ceil().RoundInt())
		}
	}

	// Before checking gas prices, remove taxed from fee
	var hasNeg bool
	if stdFee.Amount, hasNeg = stdFee.Amount.SafeSub(taxes); hasNeg {
		return sdk.ErrInsufficientFee(
			fmt.Sprintf(
				"insufficient fees; got: %q, required: %q = %q(gas) +%q(stability)", stdFee.Amount.Add(taxes), requiredFees.Add(taxes), requiredFees, taxes,
			),
		).Result()
	}

	if !requiredFees.Empty() && !stdFee.Amount.IsAnyGTE(requiredFees) {
		return sdk.ErrInsufficientFee(
			fmt.Sprintf(
				"insufficient fees; got: %q, required: %q = %q(gas) +%q(stability)", stdFee.Amount.Add(taxes), requiredFees.Add(taxes), requiredFees, taxes,
			),
		).Result()
	}

	return sdk.Result{}
}

// SetGasMeter returns a new context with a gas meter set from a given context.
func SetGasMeter(simulate bool, ctx sdk.Context, gasLimit uint64) sdk.Context {
	// In various cases such as simulation and during the genesis block, we do not
	// meter any gas utilization.
	if simulate || ctx.BlockHeight() == 0 {
		return ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
	}

	return ctx.WithGasMeter(sdk.NewGasMeter(gasLimit))
}

// GetSignBytes returns a slice of bytes to sign over for a given transaction
// and an account.
func GetSignBytes(chainID string, stdTx auth.StdTx, acc auth.Account, genesis bool) []byte {
	var accNum uint64
	if !genesis {
		accNum = acc.GetAccountNumber()
	}

	return auth.StdSignBytes(
		chainID, accNum, acc.GetSequence(), stdTx.Fee, stdTx.Msgs, stdTx.Memo,
	)
}
