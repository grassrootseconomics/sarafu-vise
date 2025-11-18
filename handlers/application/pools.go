package application

import (
	"context"
	"fmt"

	"git.grassecon.net/grassrootseconomics/sarafu-vise/config"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/go-vise/db"
	"github.com/grassrootseconomics/go-vise/resource"
	dataserviceapi "github.com/grassrootseconomics/ussd-data-service/pkg/api"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// GetPools fetches a list of 5 top pools
func (h *MenuHandlers) GetPools(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	userStore := h.userdataStore

	flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

	// call the api to get a list of top 5 pools sorted by swaps
	topPools, err := h.accountService.FetchTopPools(ctx)
	if err != nil {
		res.FlagSet = append(res.FlagSet, flag_api_call_error)
		logg.ErrorCtxf(ctx, "failed on FetchTransactions", "error", err)
		return res, err
	}

	// Return if there are no pools
	if len(topPools) == 0 {
		return res, nil
	}

	activePoolSymStr := ""

	activePoolSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_SYM)
	if err != nil {
		activePoolSymStr = config.DefaultPoolSymbol()
	} else {
		activePoolSymStr = string(activePoolSym)
	}

	// Filter out the active pool from topPools
	filteredPools := make([]dataserviceapi.PoolDetails, 0, len(topPools))
	for _, p := range topPools {
		if p.PoolSymbol != activePoolSymStr {
			filteredPools = append(filteredPools, p)
		}
	}

	data := store.ProcessPools(filteredPools)

	// Store the filtered Pool data
	dataMap := map[storedb.DataTyp]string{
		storedb.DATA_POOL_NAMES:     data.PoolNames,
		storedb.DATA_POOL_SYMBOLS:   data.PoolSymbols,
		storedb.DATA_POOL_ADDRESSES: data.PoolContractAdrresses,
	}

	// Write data entries
	for key, value := range dataMap {
		if err := userStore.WriteEntry(ctx, sessionId, key, []byte(value)); err != nil {
			logg.ErrorCtxf(ctx, "Failed to write data entry for sessionId: %s", sessionId, "key", key, "error", err)
			continue
		}
	}

	res.Content = h.ReplaceSeparatorFunc(data.PoolSymbols)

	return res, nil
}

// GetDefaultPool returns the current user's Pool. If none is set, it returns the default config pool.
func (h *MenuHandlers) GetDefaultPool(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	userStore := h.userdataStore
	activePoolSym, err := userStore.ReadEntry(ctx, sessionId, storedb.DATA_ACTIVE_POOL_SYM)
	if err != nil {
		if db.IsNotFound(err) {
			// set the default as the response
			res.Content = config.DefaultPoolSymbol()
			return res, nil
		}

		logg.ErrorCtxf(ctx, "failed to read the activePoolSym entry with", "key", storedb.DATA_ACTIVE_POOL_SYM, "error", err)
		return res, err
	}

	res.Content = string(activePoolSym)

	return res, nil
}

// ViewPool retrieves the pool details from the user store
// and displays it to the user for them to select it.
func (h *MenuHandlers) ViewPool(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	flag_incorrect_pool, _ := h.flagManager.GetFlag("flag_incorrect_pool")

	inputStr := string(input)
	if inputStr == "0" || inputStr == "99" || inputStr == "88" || inputStr == "98" {
		res.FlagReset = append(res.FlagReset, flag_incorrect_pool)
		return res, nil
	}

	poolData, err := store.GetPoolData(ctx, h.userdataStore, sessionId, inputStr)
	if err != nil {
		return res, fmt.Errorf("failed to retrieve pool data: %v", err)
	}

	if poolData == nil {
		flag_api_call_error, _ := h.flagManager.GetFlag("flag_api_call_error")

		// no match found. Call the API using the inputStr as the symbol
		poolResp, err := h.accountService.RetrievePoolDetails(ctx, inputStr)
		if err != nil {
			res.FlagSet = append(res.FlagSet, flag_api_call_error)
			return res, nil
		}

		if len(poolResp.PoolSymbol) == 0 {
			// If the API does not return the data, set the flag
			res.FlagSet = append(res.FlagSet, flag_incorrect_pool)
			return res, nil
		}

		poolData = poolResp
	}

	if err := store.StoreTemporaryPool(ctx, h.userdataStore, sessionId, poolData); err != nil {
		logg.ErrorCtxf(ctx, "failed on StoreTemporaryPool", "error", err)
		return res, err
	}

	res.FlagReset = append(res.FlagReset, flag_incorrect_pool)
	res.Content = l.Get("Name: %s\nSymbol: %s", poolData.PoolName, poolData.PoolSymbol)

	return res, nil
}

// SetPool retrieves the temp pool data and sets it as the active data.
func (h *MenuHandlers) SetPool(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	// Get temporary data
	tempData, err := store.GetTemporaryPoolData(ctx, h.userdataStore, sessionId)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed on GetTemporaryPoolData", "error", err)
		return res, err
	}

	// Set as active and clear temporary data
	if err := store.UpdatePoolData(ctx, h.userdataStore, sessionId, tempData); err != nil {
		logg.ErrorCtxf(ctx, "failed on UpdatePoolData", "error", err)
		return res, err
	}

	res.Content = tempData.PoolSymbol
	return res, nil
}
