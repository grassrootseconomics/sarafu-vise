package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"git.grassecon.net/grassrootseconomics/common/person"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/go-vise/db"
	"github.com/grassrootseconomics/go-vise/lang"
	"github.com/grassrootseconomics/go-vise/resource"
)

// SaveFirstname updates the first name in the gdbm with the provided input.
func (h *MenuHandlers) SaveFirstname(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	firstName := string(input)

	store := h.userdataStore

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_firstname_set, _ := h.flagManager.GetFlag("flag_firstname_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	firstNameSet := h.st.MatchFlag(flag_firstname_set, true)
	if allowUpdate {
		temporaryFirstName, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryFirstName) == 0 {
			logg.ErrorCtxf(ctx, "temporaryFirstName is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_FIRST_NAME, []byte(temporaryFirstName))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write firstName entry with", "key", storedb.DATA_FIRST_NAME, "value", temporaryFirstName, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_firstname_set)

	} else {
		if firstNameSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(firstName))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryFirstName entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", firstName, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(0, firstName)
		}
	}

	return res, nil
}

// SaveFamilyname updates the family name in the gdbm with the provided input.
func (h *MenuHandlers) SaveFamilyname(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	store := h.userdataStore
	familyName := string(input)

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_familyname_set, _ := h.flagManager.GetFlag("flag_familyname_set")
	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	familyNameSet := h.st.MatchFlag(flag_familyname_set, true)

	if allowUpdate {
		temporaryFamilyName, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryFamilyName) == 0 {
			logg.ErrorCtxf(ctx, "temporaryFamilyName is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME, []byte(temporaryFamilyName))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write familyName entry with", "key", storedb.DATA_FAMILY_NAME, "value", temporaryFamilyName, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_familyname_set)

	} else {
		if familyNameSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(familyName))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryFamilyName entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", familyName, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(1, familyName)
		}
	}

	return res, nil
}

// VerifyYob verifies the length of the given input.
func (h *MenuHandlers) VerifyYob(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error

	flag_incorrect_date_format, _ := h.flagManager.GetFlag("flag_incorrect_date_format")
	date := string(input)
	_, err = strconv.Atoi(date)
	if err != nil {
		// If conversion fails, input is not numeric
		res.FlagSet = append(res.FlagSet, flag_incorrect_date_format)
		return res, nil
	}

	if person.IsValidYOb(date) {
		res.FlagReset = append(res.FlagReset, flag_incorrect_date_format)
	} else {
		res.FlagSet = append(res.FlagSet, flag_incorrect_date_format)
	}
	return res, nil
}

// ResetIncorrectYob resets the incorrect date format flag after a new attempt.
func (h *MenuHandlers) ResetIncorrectYob(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result

	flag_incorrect_date_format, _ := h.flagManager.GetFlag("flag_incorrect_date_format")
	res.FlagReset = append(res.FlagReset, flag_incorrect_date_format)
	return res, nil
}

// SaveYOB updates the Year of Birth(YOB) in the gdbm with the provided input.
func (h *MenuHandlers) SaveYob(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	yob := string(input)
	store := h.userdataStore

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_yob_set, _ := h.flagManager.GetFlag("flag_yob_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	yobSet := h.st.MatchFlag(flag_yob_set, true)

	if allowUpdate {
		temporaryYob, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryYob) == 0 {
			logg.ErrorCtxf(ctx, "temporaryYob is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_YOB, []byte(temporaryYob))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write yob entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", temporaryYob, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_yob_set)

	} else {
		if yobSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(yob))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryYob entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", yob, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(3, yob)
		}
	}

	return res, nil
}

// SaveLocation updates the location in the gdbm with the provided input.
func (h *MenuHandlers) SaveLocation(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	location := string(input)
	store := h.userdataStore

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_location_set, _ := h.flagManager.GetFlag("flag_location_set")
	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	locationSet := h.st.MatchFlag(flag_location_set, true)

	if allowUpdate {
		temporaryLocation, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryLocation) == 0 {
			logg.ErrorCtxf(ctx, "temporaryLocation is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_LOCATION, []byte(temporaryLocation))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write location entry with", "key", storedb.DATA_LOCATION, "value", temporaryLocation, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_location_set)

	} else {
		if locationSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(location))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryLocation entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", location, "error", err)
				return res, err
			}
			res.FlagSet = append(res.FlagSet, flag_location_set)
		} else {
			h.profile.InsertOrShift(4, location)
		}
	}

	return res, nil
}

// SaveGender updates the gender in the gdbm with the provided input.
func (h *MenuHandlers) SaveGender(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	symbol, _ := h.st.Where()
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	gender := strings.Split(symbol, "_")[1]
	store := h.userdataStore
	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_gender_set, _ := h.flagManager.GetFlag("flag_gender_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	genderSet := h.st.MatchFlag(flag_gender_set, true)

	if allowUpdate {
		temporaryGender, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryGender) == 0 {
			logg.ErrorCtxf(ctx, "temporaryGender is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_GENDER, []byte(temporaryGender))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write gender entry with", "key", storedb.DATA_GENDER, "value", gender, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_gender_set)

	} else {
		if genderSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(gender))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryGender entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", gender, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(2, gender)
		}
	}

	return res, nil
}

// SaveOfferings updates the offerings(goods and services provided by the user) in the gdbm with the provided input.
func (h *MenuHandlers) SaveOfferings(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var err error
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	offerings := string(input)
	store := h.userdataStore

	flag_allow_update, _ := h.flagManager.GetFlag("flag_allow_update")
	flag_offerings_set, _ := h.flagManager.GetFlag("flag_offerings_set")

	allowUpdate := h.st.MatchFlag(flag_allow_update, true)
	offeringsSet := h.st.MatchFlag(flag_offerings_set, true)

	if allowUpdate {
		temporaryOfferings, _ := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
		if len(temporaryOfferings) == 0 {
			logg.ErrorCtxf(ctx, "temporaryOfferings is empty", "key", storedb.DATA_TEMPORARY_VALUE)
			return res, fmt.Errorf("Data error encountered")
		}
		err = store.WriteEntry(ctx, sessionId, storedb.DATA_OFFERINGS, []byte(temporaryOfferings))
		if err != nil {
			logg.ErrorCtxf(ctx, "failed to write offerings entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", offerings, "error", err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_offerings_set)
	} else {
		if offeringsSet {
			err = store.WriteEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE, []byte(offerings))
			if err != nil {
				logg.ErrorCtxf(ctx, "failed to write temporaryOfferings entry with", "key", storedb.DATA_TEMPORARY_VALUE, "value", offerings, "error", err)
				return res, err
			}
		} else {
			h.profile.InsertOrShift(5, offerings)
		}
	}

	return res, nil
}

// GetCurrentProfileInfo retrieves specific profile fields based on the current state of the USSD session.
// Uses flag management system to track profile field status and handle menu navigation.
func (h *MenuHandlers) GetCurrentProfileInfo(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var profileInfo []byte
	var defaultValue string
	var err error

	flag_firstname_set, _ := h.flagManager.GetFlag("flag_firstname_set")
	flag_familyname_set, _ := h.flagManager.GetFlag("flag_familyname_set")
	flag_yob_set, _ := h.flagManager.GetFlag("flag_yob_set")
	flag_gender_set, _ := h.flagManager.GetFlag("flag_gender_set")
	flag_location_set, _ := h.flagManager.GetFlag("flag_location_set")
	flag_offerings_set, _ := h.flagManager.GetFlag("flag_offerings_set")
	flag_back_set, _ := h.flagManager.GetFlag("flag_back_set")

	res.FlagReset = append(res.FlagReset, flag_back_set)

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	language, ok := ctx.Value("Language").(lang.Language)
	if !ok {
		return res, fmt.Errorf("value for 'Language' is not of type lang.Language")
	}
	code := language.Code
	if code == "swa" {
		defaultValue = "Haipo"
	} else {
		defaultValue = "Not Provided"
	}

	sm, _ := h.st.Where()
	parts := strings.SplitN(sm, "_", 2)
	filename := parts[1]
	dbKeyStr := "DATA_" + strings.ToUpper(filename)
	logg.InfoCtxf(ctx, "GetCurrentProfileInfo", "filename", filename, "dbKeyStr:", dbKeyStr)
	dbKey, err := storedb.StringToDataTyp(dbKeyStr)

	if err != nil {
		return res, err
	}
	store := h.userdataStore

	switch dbKey {
	case storedb.DATA_FIRST_NAME:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_FIRST_NAME)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read first name entry with", "key", "error", storedb.DATA_FIRST_NAME, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_firstname_set)
		res.Content = string(profileInfo)
	case storedb.DATA_FAMILY_NAME:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read family name entry with", "key", "error", storedb.DATA_FAMILY_NAME, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_familyname_set)
		res.Content = string(profileInfo)

	case storedb.DATA_GENDER:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_GENDER)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read gender entry with", "key", "error", storedb.DATA_GENDER, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_gender_set)
		res.Content = string(profileInfo)
	case storedb.DATA_YOB:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_YOB)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read year of birth(yob) entry with", "key", "error", storedb.DATA_YOB, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_yob_set)
		res.Content = string(profileInfo)
	case storedb.DATA_LOCATION:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_LOCATION)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read location entry with", "key", "error", storedb.DATA_LOCATION, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_location_set)
		res.Content = string(profileInfo)
	case storedb.DATA_OFFERINGS:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_OFFERINGS)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read offerings entry with", "key", "error", storedb.DATA_OFFERINGS, err)
			return res, err
		}
		res.FlagSet = append(res.FlagSet, flag_offerings_set)
		res.Content = string(profileInfo)
	case storedb.DATA_ACCOUNT_ALIAS:
		profileInfo, err = store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS)
		if err != nil {
			if db.IsNotFound(err) {
				res.Content = defaultValue
				break
			}
			logg.ErrorCtxf(ctx, "Failed to read account alias entry with", "key", "error", storedb.DATA_ACCOUNT_ALIAS, err)
			return res, err
		}
		alias := string(profileInfo)
		if alias == "" {
			res.Content = defaultValue
		} else {
			res.Content = alias
		}
	default:
		break
	}

	return res, nil
}

// GetProfileInfo provides a comprehensive view of a user's profile.
func (h *MenuHandlers) GetProfileInfo(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	var defaultValue string
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	language, ok := ctx.Value("Language").(lang.Language)
	if !ok {
		return res, fmt.Errorf("value for 'Language' is not of type lang.Language")
	}
	code := language.Code
	if code == "swa" {
		defaultValue = "Haipo"
	} else {
		defaultValue = "Not Provided"
	}

	// Helper function to handle nil byte slices and convert them to string
	getEntryOrDefault := func(entry []byte, err error) string {
		if err != nil || entry == nil {
			return defaultValue
		}
		return string(entry)
	}
	store := h.userdataStore
	// Retrieve user data as strings with fallback to defaultValue
	firstName := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_FIRST_NAME))
	familyName := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_FAMILY_NAME))
	yob := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_YOB))
	gender := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_GENDER))
	location := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_LOCATION))
	offerings := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_OFFERINGS))
	alias := getEntryOrDefault(store.ReadEntry(ctx, sessionId, storedb.DATA_ACCOUNT_ALIAS))

	if alias != defaultValue && alias != "" {
		alias = strings.Split(alias, ".")[0]
	} else {
		alias = defaultValue
	}

	// Construct the full name
	name := person.ConstructName(firstName, familyName, defaultValue)

	// Calculate age from year of birth
	age := defaultValue
	if yob != defaultValue {
		if yobInt, err := strconv.Atoi(yob); err == nil {
			age = strconv.Itoa(person.CalculateAgeWithYOB(yobInt))
		} else {
			return res, fmt.Errorf("invalid year of birth: %v", err)
		}
	}
	switch language.Code {
	case "eng":
		res.Content = fmt.Sprintf(
			"Name: %s\nGender: %s\nAge: %s\nLocation: %s\nYou provide: %s\nYour alias: %s\n",
			name, gender, age, location, offerings, alias,
		)
	case "swa":
		res.Content = fmt.Sprintf(
			"Jina: %s\nJinsia: %s\nUmri: %s\nEneo: %s\nUnauza: %s\nLakabu yako: %s\n",
			name, gender, age, location, offerings, alias,
		)
	default:
		res.Content = fmt.Sprintf(
			"Name: %s\nGender: %s\nAge: %s\nLocation: %s\nYou provide: %s\nYour alias: %s\n",
			name, gender, age, location, offerings, alias,
		)
	}

	return res, nil
}

// handles bulk updates of profile information.
func (h *MenuHandlers) insertProfileItems(ctx context.Context, sessionId string, res *resource.Result) error {
	var err error
	userStore := h.userdataStore
	profileFlagNames := []string{
		"flag_firstname_set",
		"flag_familyname_set",
		"flag_yob_set",
		"flag_gender_set",
		"flag_location_set",
		"flag_offerings_set",
	}
	profileDataKeys := []storedb.DataTyp{
		storedb.DATA_FIRST_NAME,
		storedb.DATA_FAMILY_NAME,
		storedb.DATA_GENDER,
		storedb.DATA_YOB,
		storedb.DATA_LOCATION,
		storedb.DATA_OFFERINGS,
	}
	for index, profileItem := range h.profile.ProfileItems {
		// Ensure the profileItem is not "0"(is set)
		if profileItem != "0" {
			flag, _ := h.flagManager.GetFlag(profileFlagNames[index])
			isProfileItemSet := h.st.MatchFlag(flag, true)
			if !isProfileItemSet {
				err = userStore.WriteEntry(ctx, sessionId, profileDataKeys[index], []byte(profileItem))
				if err != nil {
					logg.ErrorCtxf(ctx, "failed to write profile entry with", "key", profileDataKeys[index], "value", profileItem, "error", err)
					return err
				}
				res.FlagSet = append(res.FlagSet, flag)
			}
		}
	}
	return nil
}

// UpdateAllProfileItems  is used to persist all the  new profile information and setup  the required profile flags.
func (h *MenuHandlers) UpdateAllProfileItems(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}
	err := h.insertProfileItems(ctx, sessionId, &res)
	if err != nil {
		return res, err
	}
	return res, nil
}
