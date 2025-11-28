package handlers

import (
	"context"
	"strings"

	"git.defalsify.org/vise.git/db"
	"git.defalsify.org/vise.git/engine"
	"git.defalsify.org/vise.git/logging"
	"git.defalsify.org/vise.git/persist"
	"git.defalsify.org/vise.git/resource"

	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/handlers/application"
)

var (
	logg = logging.NewVanilla().WithDomain("sarafu-vise.engine")
)

type HandlerService interface {
	GetHandler() (*application.MenuHandlers, error)
}

type LocalHandlerService struct {
	Parser        *application.FlagManager
	DbRs          *resource.DbResource
	Pe            *persist.Persister
	UserdataStore *db.Db
	LogDb         *db.Db
	Cfg           engine.Config
	Rs            resource.Resource
	first         resource.EntryFunc
}

func NewLocalHandlerService(ctx context.Context, fp string, debug bool, dbResource *resource.DbResource, cfg engine.Config, rs resource.Resource) (*LocalHandlerService, error) {
	parser, err := application.NewFlagManager(fp)
	if err != nil {
		return nil, err
	}
	if debug {
		parser.SetDebug()
	}

	return &LocalHandlerService{
		Parser: parser,
		DbRs:   dbResource,
		Cfg:    cfg,
		Rs:     rs,
	}, nil
}

func (ls *LocalHandlerService) SetPersister(Pe *persist.Persister) {
	ls.Pe = Pe
}

func (ls *LocalHandlerService) SetDataStore(db *db.Db) {
	ls.UserdataStore = db
}

func (ls *LocalHandlerService) SetLogDb(db *db.Db) {
	ls.LogDb = db
}

func (ls *LocalHandlerService) GetHandler(accountService remote.AccountService) (*application.MenuHandlers, error) {
	replaceSeparatorFunc := func(input string) string {
		return strings.ReplaceAll(input, ":", ls.Cfg.MenuSeparator)
	}

	appHandlers, err := application.NewMenuHandlers(ls.Parser, *ls.UserdataStore, *ls.LogDb, accountService, replaceSeparatorFunc)
	if err != nil {
		return nil, err
	}
	appHandlers.SetPersister(ls.Pe)
	ls.DbRs.AddLocalFunc("check_blocked_status", appHandlers.CheckBlockedStatus)
	ls.DbRs.AddLocalFunc("set_language", appHandlers.SetLanguage)
	ls.DbRs.AddLocalFunc("create_account", appHandlers.CreateAccount)
	ls.DbRs.AddLocalFunc("save_temporary_pin", appHandlers.SaveTemporaryPin)
	ls.DbRs.AddLocalFunc("verify_create_pin", appHandlers.VerifyCreatePin)
	ls.DbRs.AddLocalFunc("check_identifier", appHandlers.CheckIdentifier)
	ls.DbRs.AddLocalFunc("check_account_status", appHandlers.CheckAccountStatus)
	ls.DbRs.AddLocalFunc("authorize_account", appHandlers.Authorize)
	ls.DbRs.AddLocalFunc("quit", appHandlers.Quit)
	ls.DbRs.AddLocalFunc("check_balance", appHandlers.CheckBalance)
	ls.DbRs.AddLocalFunc("validate_recipient", appHandlers.ValidateRecipient)
	ls.DbRs.AddLocalFunc("transaction_reset", appHandlers.TransactionReset)
	ls.DbRs.AddLocalFunc("invite_valid_recipient", appHandlers.InviteValidRecipient)
	ls.DbRs.AddLocalFunc("send_max_amount", appHandlers.MaxAmount)
	ls.DbRs.AddLocalFunc("credit_max_amount", appHandlers.MaxAmount)
	ls.DbRs.AddLocalFunc("validate_amount", appHandlers.ValidateAmount)
	ls.DbRs.AddLocalFunc("reset_transaction_amount", appHandlers.ResetTransactionAmount)
	ls.DbRs.AddLocalFunc("get_recipient", appHandlers.GetRecipient)
	ls.DbRs.AddLocalFunc("get_sender", appHandlers.GetSender)
	ls.DbRs.AddLocalFunc("get_amount", appHandlers.GetAmount)
	ls.DbRs.AddLocalFunc("reset_incorrect_pin", appHandlers.ResetIncorrectPin)
	ls.DbRs.AddLocalFunc("save_firstname", appHandlers.SaveFirstname)
	ls.DbRs.AddLocalFunc("save_familyname", appHandlers.SaveFamilyname)
	ls.DbRs.AddLocalFunc("save_gender", appHandlers.SaveGender)
	ls.DbRs.AddLocalFunc("save_location", appHandlers.SaveLocation)
	ls.DbRs.AddLocalFunc("save_yob", appHandlers.SaveYob)
	ls.DbRs.AddLocalFunc("save_offerings", appHandlers.SaveOfferings)
	ls.DbRs.AddLocalFunc("reset_account_authorized", appHandlers.ResetAccountAuthorized)
	ls.DbRs.AddLocalFunc("reset_allow_update", appHandlers.ResetAllowUpdate)
	ls.DbRs.AddLocalFunc("get_profile_info", appHandlers.GetProfileInfo)
	ls.DbRs.AddLocalFunc("verify_yob", appHandlers.VerifyYob)
	ls.DbRs.AddLocalFunc("reset_incorrect_date_format", appHandlers.ResetIncorrectYob)
	ls.DbRs.AddLocalFunc("initiate_transaction", appHandlers.InitiateTransaction)
	ls.DbRs.AddLocalFunc("confirm_pin_change", appHandlers.ConfirmPinChange)
	ls.DbRs.AddLocalFunc("quit_with_help", appHandlers.QuitWithHelp)
	ls.DbRs.AddLocalFunc("fetch_community_balance", appHandlers.FetchCommunityBalance)
	ls.DbRs.AddLocalFunc("manage_vouchers", appHandlers.ManageVouchers)
	ls.DbRs.AddLocalFunc("get_vouchers", appHandlers.GetVoucherList)
	ls.DbRs.AddLocalFunc("view_voucher", appHandlers.ViewVoucher)
	ls.DbRs.AddLocalFunc("set_voucher", appHandlers.SetVoucher)
	ls.DbRs.AddLocalFunc("get_voucher_details", appHandlers.GetVoucherDetails)
	ls.DbRs.AddLocalFunc("get_default_pool", appHandlers.GetDefaultPool)
	ls.DbRs.AddLocalFunc("get_pools", appHandlers.GetPools)
	ls.DbRs.AddLocalFunc("view_pool", appHandlers.ViewPool)
	ls.DbRs.AddLocalFunc("set_pool", appHandlers.SetPool)
	ls.DbRs.AddLocalFunc("validate_blocked_number", appHandlers.ValidateBlockedNumber)
	ls.DbRs.AddLocalFunc("retrieve_blocked_number", appHandlers.RetrieveBlockedNumber)
	ls.DbRs.AddLocalFunc("reset_unregistered_number", appHandlers.ResetUnregisteredNumber)
	ls.DbRs.AddLocalFunc("reset_others_pin", appHandlers.ResetOthersPin)
	ls.DbRs.AddLocalFunc("get_current_profile_info", appHandlers.GetCurrentProfileInfo)
	ls.DbRs.AddLocalFunc("check_transactions", appHandlers.CheckTransactions)
	ls.DbRs.AddLocalFunc("get_transactions", appHandlers.GetTransactionsList)
	ls.DbRs.AddLocalFunc("view_statement", appHandlers.ViewTransactionStatement)
	ls.DbRs.AddLocalFunc("update_all_profile_items", appHandlers.UpdateAllProfileItems)
	ls.DbRs.AddLocalFunc("set_back", appHandlers.SetBack)
	ls.DbRs.AddLocalFunc("show_blocked_account", appHandlers.ShowBlockedAccount)
	ls.DbRs.AddLocalFunc("clear_temporary_value", appHandlers.ClearTemporaryValue)
	ls.DbRs.AddLocalFunc("reset_invalid_pin", appHandlers.ResetInvalidPIN)
	ls.DbRs.AddLocalFunc("request_custom_alias", appHandlers.RequestCustomAlias)
	ls.DbRs.AddLocalFunc("check_account_created", appHandlers.CheckAccountCreated)
	ls.DbRs.AddLocalFunc("reset_api_call_failure", appHandlers.ResetApiCallFailure)
	ls.DbRs.AddLocalFunc("swap_to_list", appHandlers.LoadSwapToList)
	ls.DbRs.AddLocalFunc("swap_max_limit", appHandlers.SwapMaxLimit)
	ls.DbRs.AddLocalFunc("swap_preview", appHandlers.SwapPreview)
	ls.DbRs.AddLocalFunc("initiate_swap", appHandlers.InitiateSwap)
	ls.DbRs.AddLocalFunc("transaction_swap_preview", appHandlers.TransactionSwapPreview)
	ls.DbRs.AddLocalFunc("transaction_initiate_swap", appHandlers.TransactionInitiateSwap)
	ls.DbRs.AddLocalFunc("clear_trans_type_flag", appHandlers.ClearTransactionTypeFlag)
	ls.DbRs.AddLocalFunc("get_mpesa_max_limit", appHandlers.GetMpesaMaxLimit)
	ls.DbRs.AddLocalFunc("get_mpesa_preview", appHandlers.GetMpesaPreview)
	ls.DbRs.AddLocalFunc("initiate_get_mpesa", appHandlers.InitiateGetMpesa)
	ls.DbRs.AddLocalFunc("send_mpesa_min_limit", appHandlers.SendMpesaMinLimit)
	ls.DbRs.AddLocalFunc("send_mpesa_preview", appHandlers.SendMpesaPreview)
	ls.DbRs.AddLocalFunc("initiate_send_mpesa", appHandlers.InitiateSendMpesa)

	ls.first = appHandlers.Init

	return appHandlers, nil
}

func (ls *LocalHandlerService) GetEngine(cfg engine.Config, rs resource.Resource, pr *persist.Persister) engine.Engine {
	en := engine.NewEngine(cfg, rs)
	if ls.first != nil {
		en = en.WithFirst(ls.first)
	}
	en = en.WithPersister(pr)
	if cfg.EngineDebug {
		en = en.WithDebug(nil)
	}
	return en
}
