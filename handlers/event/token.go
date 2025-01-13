package event

import (
	"context"
	"strings"

	"git.defalsify.org/vise.git/db"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"git.grassecon.net/grassrootseconomics/common/identity"
	apievent "git.grassecon.net/grassrootseconomics/sarafu-api/event"
)

// execute all 
func (eh *EventsHandler) updateToken(ctx context.Context, userStore *store.UserDataStore, identity identity.Identity, tokenAddress string) error {
	err := eh.updateTokenList(ctx, userStore, identity)
	if err != nil {
		return err
	}

	userStore.Db.SetSession(identity.SessionId)
	activeSym, err := userStore.ReadEntry(ctx, identity.SessionId, storedb.DATA_ACTIVE_SYM)
	if err == nil {
		return nil
	}
	if !db.IsNotFound(err) {
		return err
	}
	if activeSym == nil {
		activeSym, err = eh.toSym(ctx, tokenAddress)
		if err != nil {
			return err
		}
	}

	err = updateDefaultToken(ctx, userStore, identity, string(activeSym))
	if err != nil {
		return err
	}

	err = eh.updateTokenTransferList(ctx, userStore, identity)
	if err != nil {
		return err
	}

	return nil
}


// set default token to given symbol.
func updateDefaultToken(ctx context.Context, userStore *store.UserDataStore, identity identity.Identity, activeSym string) error {
	pfxDb := toPrefixDb(userStore, identity.SessionId)
	// TODO: the activeSym input should instead be newline separated list?
	tokenData, err := store.GetVoucherData(ctx, pfxDb, activeSym)
	if err != nil {
		return err
	}
	return store.UpdateVoucherData(ctx, userStore, identity.SessionId, tokenData)
}


// handle token transfer.
//
// if from and to are NOT the same, handle code will be executed once for each side of the transfer.
func (eh *EventsHandler) HandleTokenTransfer(ctx context.Context, userStore *store.UserDataStore, ev *apievent.EventTokenTransfer) error {
	identity, err := store.IdentityFromAddress(ctx, userStore, ev.From)
	if err != nil {
		if !db.IsNotFound(err) {
			return err
		}
	} else {
		err = eh.updateToken(ctx, userStore, identity, ev.VoucherAddress)
		if err != nil {
			return err
		}
	}

	if strings.Compare(ev.To, ev.From) != 0 {
		identity, err = store.IdentityFromAddress(ctx, userStore, ev.To)
		if err != nil {
			if !db.IsNotFound(err) {
				return err
			}
		} else {
			err = eh.updateToken(ctx, userStore, identity, ev.VoucherAddress)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// handle token mint.
func (eh *EventsHandler) HandleTokenMint(ctx context.Context, userStore *store.UserDataStore, ev *apievent.EventTokenMint) error {
	identity, err := store.IdentityFromAddress(ctx, userStore, ev.To)
	if err != nil {
		if !db.IsNotFound(err) {
			return err
		}
	} else {
		err = eh.updateToken(ctx, userStore, identity, ev.VoucherAddress)
		if err != nil {
			return err
		}
	}
	return nil
}

// use api to resolve address to token symbol.
func (ev *EventsHandler) toSym(ctx context.Context, address string) ([]byte, error) {
	voucherData, err := ev.api.VoucherData(ctx, address)
	if err != nil {
		return nil, err
	}
	return []byte(voucherData.TokenSymbol), nil
}

// refresh and store token list.
func (eh *EventsHandler) updateTokenList(ctx context.Context, userStore *store.UserDataStore, identity identity.Identity) error {
	holdings, err := eh.api.FetchVouchers(ctx, identity.ChecksumAddress)
	if err != nil {
		return err
	}
	metadata := store.ProcessVouchers(holdings)
	_ = metadata

	// TODO: make sure subprefixdb is thread safe when using gdbm
	// TODO: why is address session here unless explicitly set
	pfxDb := toPrefixDb(userStore, identity.SessionId)

	typ := storedb.ToBytes(storedb.DATA_VOUCHER_SYMBOLS)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Symbols))
	if err != nil {
		return err
	}

	typ = storedb.ToBytes(storedb.DATA_VOUCHER_BALANCES)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Balances))
	if err != nil {
		return err
	}

	typ = storedb.ToBytes(storedb.DATA_VOUCHER_DECIMALS)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Decimals))
	if err != nil {
		return err
	}

	typ = storedb.ToBytes(storedb.DATA_VOUCHER_ADDRESSES)
	err = pfxDb.Put(ctx, typ, []byte(metadata.Addresses))
	if err != nil {
		return err
	}

	return nil
}

// refresh and store transaction history.
func (eh *EventsHandler) updateTokenTransferList(ctx context.Context, userStore *store.UserDataStore, identity identity.Identity) error {
	var r []string

	txs, err := eh.api.FetchTransactions(ctx, identity.ChecksumAddress)
	if err != nil {
		return err
	}

	for i, tx := range(txs) {
		//r = append(r, formatTransaction(i, tx))
		r = append(r, eh.formatFunc(apievent.EventTokenTransferTag, i, tx))
	}

	s := strings.Join(r, "\n")

	return userStore.WriteEntry(ctx, identity.SessionId, storedb.DATA_TRANSACTIONS, []byte(s))
}

func toPrefixDb(userStore *store.UserDataStore, sessionId string) storedb.PrefixDb {
	userStore.Db.SetSession(sessionId)
	prefix := storedb.ToBytes(db.DATATYPE_USERDATA)
	return store.StoreToPrefixDb(userStore, prefix)
}
