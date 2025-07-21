package application

import (
	"context"
	"fmt"
	"strings"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/resource"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// ManageVouchers retrieves the token holdings from the API using the "PublicKey" and
// 1. sets the first as the default voucher if no active voucher is set.
// 2. Stores list of vouchers
// 3. updates the balance of the active voucher
func (h *MenuHandlers) ManageVouchers(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	userStore := h.userdataStore
	logdb := h.logDb

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	flag_no_active_voucher, _ := h.flagManager.GetFlag("flag_no_active_voucher")
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	publicKey, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return res, err
	}

	// Fetch vouchers from API
	vouchersResp, err := h.accountService.FetchVouchers(ctx, string(publicKey))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		return res, nil
	}
	res.FlagReset = append(res.FlagReset, flag_api_error)

	if len(vouchersResp) == 0 {
		res.FlagSet = append(res.FlagSet, flag_no_active_voucher)
		return res, nil
	}

	res.FlagReset = append(res.FlagReset, flag_no_active_voucher)

	// add a variable to filter out the active voucher
	activeSymStr := ""

	// Check if user has an active voucher with proper error handling
	activeSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_SYM)
	if err != nil {
		if db.IsNotFound(err) {
			// No active voucher, set the first one as default
			firstVoucher := vouchersResp[0]
			defaultSym := firstVoucher.TokenSymbol
			defaultBal := firstVoucher.Balance
			defaultDec := firstVoucher.TokenDecimals
			defaultAddr := firstVoucher.TokenAddress

			activeSymStr = defaultSym

			// Scale down the balance
			scaledBalance := store.ScaleDownBalance(defaultBal, defaultDec)

			firstVoucherMap := map[storedb.DataTyp]string{
				storedb.DATA_ACTIVE_SYM:     defaultSym,
				storedb.DATA_ACTIVE_BAL:     scaledBalance,
				storedb.DATA_ACTIVE_DECIMAL: defaultDec,
				storedb.DATA_ACTIVE_ADDRESS: defaultAddr,
			}

			for key, value := range firstVoucherMap {
				if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
					logg.ErrorCtxf(ctx, "Failed to write active voucher data", "key", key, "error", err)
					return res, err
				}
				err = logdb.WriteLogEntry(ctx, sessionId, key, []byte(value))
				if err != nil {
					logg.DebugCtxf(ctx, "Failed to write voucher db log entry", "key", key, "value", value)
				}
			}

			logg.InfoCtxf(ctx, "Default voucher set", "symbol", defaultSym, "balance", defaultBal, "decimals", defaultDec, "address", defaultAddr)
		} else {
			logg.ErrorCtxf(ctx, "failed to read activeSym entry with", "key", storedb.DATA_ACTIVE_SYM, "error", err)
			return res, err
		}
	} else {
		// Find the matching voucher data
		activeSymStr = string(activeSym)
		var activeData *dataserviceapi.TokenHoldings
		for _, voucher := range vouchersResp {
			if voucher.TokenSymbol == activeSymStr {
				activeData = &voucher
				break
			}
		}

		if activeData == nil {
			logg.InfoCtxf(ctx, "activeSym not found in vouchers, setting the first voucher as the default", "activeSym", activeSymStr)
			firstVoucher := vouchersResp[0]
			activeData = &firstVoucher
			activeSymStr = string(activeData.TokenSymbol)
		}

		// Scale down the balance
		scaledBalance := store.ScaleDownBalance(activeData.Balance, activeData.TokenDecimals)

		// Update the balance field with the scaled value
		activeData.Balance = scaledBalance

		// Pass the matching voucher data to UpdateVoucherData
		if err := store.UpdateVoucherData(ctx, h.userdataStore, sessionId, activeData); err != nil {
			logg.ErrorCtxf(ctx, "failed on UpdateVoucherData", "error", err)
			return res, err
		}
	}

	// Filter out the active voucher from vouchersResp
	filteredVouchers := make([]dataserviceapi.TokenHoldings, 0, len(vouchersResp))
	for _, v := range vouchersResp {
		if v.TokenSymbol != activeSymStr {
			filteredVouchers = append(filteredVouchers, v)
		}
	}

	// Store all voucher data (excluding the current active voucher)
	data := store.ProcessVouchers(filteredVouchers)

	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_VOUCHER_SYMBOLS:   data.Symbols,
		storedb.DATA_VOUCHER_BALANCES:  data.Balances,
		storedb.DATA_VOUCHER_DECIMALS:  data.Decimals,
		storedb.DATA_VOUCHER_ADDRESSES: data.Addresses,
	}

	// Write data entries
	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	return res, nil
}

// GetVoucherList fetches the list of vouchers from the store and formats them.
func (h *MenuHandlers) GetVoucherList(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore

	// Read vouchers from the store
	voucherData, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_VOUCHER_SYMBOLS)
	logg.InfoCtxf(ctx, "reading voucherData in GetVoucherList", "sessionId", sessionId, "key", storedb.DATA_VOUCHER_SYMBOLS, "voucherData", voucherData)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read voucherData entires with", "key", storedb.DATA_VOUCHER_SYMBOLS, "error", err)
		return res, err
	}

	voucherBalances, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_VOUCHER_BALANCES)
	logg.InfoCtxf(ctx, "reading voucherBalances in GetVoucherList", "sessionId", sessionId, "key", storedb.DATA_VOUCHER_BALANCES, "voucherBalances", voucherBalances)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read voucherData entires with", "key", storedb.DATA_VOUCHER_BALANCES, "error", err)
		return res, err
	}

	formattedVoucherList := store.FormatVoucherList(ctx, string(voucherData), string(voucherBalances))
	finalOutput := strings.Join(formattedVoucherList, "\n")

	logg.InfoCtxf(ctx, "final output for GetVoucherList", "sessionId", sessionId, "finalOutput", finalOutput)

	res.Content = finalOutput

	return res, nil
}

// ViewVoucher retrieves the token holding and balance from the subprefixDB
// and displays it to the user for them to select it.
func (h *MenuHandlers) ViewVoucher(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_incorrect_voucher, _ := h.flagManager.GetFlag("flag_incorrect_voucher")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "88" || inputStr == "98" {
		res.FlagReset = append(res.FlagReset, flag_incorrect_voucher)
		return res, nil
	}

	metadata, err := store.GetVoucherData(ctx, h.userdataStore, sessionId, inputStr)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve voucher data: %v", err)
	}

	if metadata == nil {
		res.FlagSet = append(res.FlagSet, flag_incorrect_voucher)
		return res, nil
	}

	if err := store.StoreTemporaryVoucher(ctx, h.userdataStore, sessionId, metadata); err != nil {
		logg.ErrorCtxf(ctx, "failed on StoreTemporaryVoucher", "error", err)
		return res, err
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_voucher)
	res.Content = l.Get("Symbol: %s\nBalance: %s", metadata.TokenSymbol, metadata.Balance)

	return res, nil
}

// SetVoucher retrieves the temp voucher data and sets it as the active data.
func (h *MenuHandlers) SetVoucher(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// Get temporary data
	tempData, err := store.GetTemporaryVoucherData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTemporaryVoucherData", "error", err)
		return res, err
	}

	// Set as active and clear temporary data
	if err := store.UpdateVoucherData(ctx, h.userdataStore, sessionId, tempData); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdateVoucherData", "error", err)
		return res, err
	}

	res.Content = tempData.TokenSymbol
	return res, nil
}

// GetVoucherDetails retrieves the voucher details.
func (h *MenuHandlers) GetVoucherDetails(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	flag_api_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	// get the active address
	activeAddress, err := store.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_ADDRESS)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read activeAddress entry with", "key", storedb.DATA_ACTIVE_ADDRESS, "error", err)
		return res, err
	}

	// use the voucher contract address to get the data from the API
	voucherData, err := h.accountService.VoucherData(ctx, string(activeAddress))
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_error)
		return res, nil
	}
	res.FlagReset = append(res.FlagReset, flag_api_error)

	res.Content = fmt.Sprintf(
		"Name: %s\nSymbol: %s\nCommodity: %s\nLocation: %s", voucherData.TokenName, voucherData.TokenSymbol, voucherData.TokenCommodity, voucherData.TokenLocation,
	)

	return res, nil
}
