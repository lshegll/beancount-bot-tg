package crud

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/LucaBernstein/beancount-bot-tg/helpers"
	tb "gopkg.in/tucnak/telebot.v2"
)

func (r *Repo) EnrichUserData(m *tb.Message) error {
	if m == nil {
		return fmt.Errorf("provided message was nil")
	}
	tgChatId := m.Chat.ID
	tgUserId := m.Sender.ID
	tgUsername := m.Sender.Username

	userCachePrune()
	ce, err := r.getUser(m.Chat.ID)
	if err != nil {
		return err
	}
	if ce == nil {
		LogDbf(r, helpers.TRACE, m, "Creating user for the first time in the 'auth::user' db table")
		_, err := r.db.Exec(`INSERT INTO "auth::user" ("tgChatId", "tgUserId", "tgUsername")
			VALUES ($1, $2, $3);`, tgChatId, tgUserId, tgUsername)
		return err
	}
	// Check whether some changeable attributes differ
	if ce.TgUsername != m.Sender.Username {
		LogDbf(r, helpers.TRACE, m, "Updating attributes of user in table 'auth::user' (%s, %s)", ce.TgUsername, m.Sender.Username)
		_, err := r.db.Exec(`UPDATE "auth::user" SET "tgUserId" = $2, "tgUsername" = $3 WHERE "tgChatId" = $1`, tgChatId, tgUserId, tgUsername)
		return err
	}
	return nil
}

// User cache

type User struct {
	TgChatId   int64
	TgUserId   int
	TgUsername string
}

type UserCacheEntry struct {
	Expiry time.Time
	Value  *User
}

const CACHE_VALIDITY = 15 * time.Minute

var USER_CACHE = make(map[int64]*UserCacheEntry)

func userCachePrune() {
	for i, ce := range USER_CACHE {
		if ce.Expiry.Before(time.Now()) {
			delete(USER_CACHE, i)
		}
	}
}

func (r *Repo) getUser(id int64) (*User, error) {
	value, ok := USER_CACHE[id]
	if ok {
		return value.Value, nil
	}
	rows, err := r.db.Query(`
		SELECT "tgUserId", "tgUsername"
		FROM "auth::user"
		WHERE "tgChatId" = $1
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tgUserId int
	var tgUsername sql.NullString
	if rows.Next() {
		err = rows.Scan(&tgUserId, &tgUsername)
		if err != nil {
			return nil, err
		}
		if !tgUsername.Valid {
			tgUsername.String = ""
		}
		user := &User{TgUserId: tgUserId, TgChatId: id, TgUsername: tgUsername.String}
		USER_CACHE[id] = &UserCacheEntry{Value: user, Expiry: time.Now().Add(CACHE_VALIDITY)}
		return user, nil
	}
	return nil, nil
}

const DEFAULT_CURRENCY = "EUR"

func (r *Repo) UserGetCurrency(m *tb.Message) string {
	currencyCacheKey := helpers.USERSET_CUR

	rows, err := r.db.Query(`
		SELECT "value"
		FROM "bot::userSetting"
		WHERE "tgChatId" = $1 AND "setting" = $2
	`, m.Chat.ID, currencyCacheKey)
	if err != nil {
		LogDbf(r, helpers.ERROR, m, "Encountered error while getting user currency: %s", err.Error())
	}
	defer rows.Close()

	var currency sql.NullString
	if rows.Next() {
		err = rows.Scan(&currency)
		if err != nil {
			LogDbf(r, helpers.ERROR, m, "Encountered error while scanning user currency into var: %s", err.Error())
		}
		if currency.Valid && currency.String != "" {
			return currency.String
		}
	}
	return DEFAULT_CURRENCY
}

func (r *Repo) UserIsAdmin(m *tb.Message) bool {
	adminCacheKey := helpers.USERSET_ADM
	rows, err := r.db.Query(`
		SELECT "value"
		FROM "bot::userSetting"
		WHERE "tgChatId" = $1 AND "setting" = $2
	`, m.Chat.ID, adminCacheKey)
	if err != nil {
		LogDbf(r, helpers.ERROR, m, "Encountered error while getting user isAdmin flag: %s", err.Error())
	}
	defer rows.Close()

	var isAdmin *sql.NullString
	if rows.Next() {
		err = rows.Scan(&isAdmin)
		if err != nil {
			LogDbf(r, helpers.ERROR, m, "Encountered error while scanning user isAdmin into var: %s", err.Error())
			return false
		}
		isAdminB, err := strconv.ParseBool(isAdmin.String)
		if err != nil {
			LogDbf(r, helpers.ERROR, m, "Encountered error while parsing isAdmin setting value: %s", err.Error())
			return false
		}
		if isAdmin.Valid && isAdminB {
			return true
		}
	}
	return false
}

func (r *Repo) IndividualsWithNotifications(chatId string) (recipients []string) {
	query := `
		SELECT "tgChatId"
		FROM "auth::user"
		WHERE "tgUserId" = "tgChatId" -- is a private chat
	`
	params := []interface{}{}

	if chatId != "" {
		i, err := strconv.ParseInt(chatId, 10, 64)
		if err != nil {
			LogDbf(r, helpers.ERROR, nil, "Error while parsing chatId to int64: %s", err.Error())
		}
		query += `AND "tgChatId" = $1`
		params = append(params, i)
	}
	rows, err := r.db.Query(query, params...)
	if err != nil {
		LogDbf(r, helpers.ERROR, nil, "Encountered error while getting user currency: %s", err.Error())
	}
	defer rows.Close()

	var rec string
	for rows.Next() {
		err = rows.Scan(&rec)
		if err != nil {
			LogDbf(r, helpers.ERROR, nil, "Encountered error while scanning into var: %s", err.Error())
			return []string{}
		}
		recipients = append(recipients, rec)
	}
	return
}

func (r *Repo) UserSetCurrency(m *tb.Message, currency string) error {
	currencyCacheKey := helpers.USERSET_CUR

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("could not create db tx for setting currency: %s", err.Error())
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM "bot::userSetting" WHERE "tgChatId" = $1 AND "setting" = $2`, m.Chat.ID, currencyCacheKey)
	if err != nil {
		return fmt.Errorf("could not delete setting currency: %s", err.Error())
	}
	if currency != "" {
		_, err = tx.Exec(`INSERT INTO "bot::userSetting" ("tgChatId", "setting", "value") VALUES ($1, $2, $3)`, m.Chat.ID, currencyCacheKey, currency)
		if err != nil {
			return fmt.Errorf("could not insert setting currency: %s", err.Error())
		}
	}

	tx.Commit()
	return nil
}

func (r *Repo) UserGetTag(m *tb.Message) string {
	vacationTagCacheKey := helpers.USERSET_TAG
	rows, err := r.db.Query(`
		SELECT "value"
		FROM "bot::userSetting"
		WHERE "tgChatId" = $1 AND "setting" = $2
	`, m.Chat.ID, vacationTagCacheKey)
	if err != nil {
		LogDbf(r, helpers.ERROR, m, "Encountered error while getting user tag: %s", err.Error())
	}
	defer rows.Close()

	var tag sql.NullString
	if rows.Next() {
		err = rows.Scan(&tag)
		if err != nil {
			LogDbf(r, helpers.ERROR, m, "Encountered error while scanning user tag into var: %s", err.Error())
		}
		if tag.Valid && tag.String != "" {
			return tag.String
		}
	}
	return ""
}

func (r *Repo) UserSetTag(m *tb.Message, tag string) error {
	vacationTagCacheKey := helpers.USERSET_TAG

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("could not create db tx for setting vacation tag: %s", err.Error())
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM "bot::userSetting" WHERE "tgChatId" = $1 AND "setting" = $2`, m.Chat.ID, vacationTagCacheKey)
	if err != nil {
		return fmt.Errorf("could not delete setting vacation tag: %s", err.Error())
	}
	if tag != "" {
		_, err = tx.Exec(`INSERT INTO "bot::userSetting" ("tgChatId", "setting", "value") VALUES ($1, $2, $3)`, m.Chat.ID, vacationTagCacheKey, tag)
		if err != nil {
			return fmt.Errorf("could not insert setting vacation tag: %s", err.Error())
		}
	}

	tx.Commit()
	return nil
}

func (r *Repo) UserGetNotificationSetting(m *tb.Message) (daysDelay, hour int, err error) {
	rows, err := r.db.Query(`
		SELECT "delayHours", "notificationHour"
		FROM "bot::notificationSchedule"
		WHERE "tgChatId" = $1
	`, m.Chat.ID)
	if err != nil {
		LogDbf(r, helpers.ERROR, m, "Encountered error while getting user notification setting: %s", err.Error())
	}
	defer rows.Close()

	var delayHours int
	if rows.Next() {
		err = rows.Scan(&delayHours, &hour)
		if err != nil {
			LogDbf(r, helpers.ERROR, m, "Encountered error while scanning user notification setting into var: %s", err.Error())
		}
		return delayHours / 24, hour, nil
	}
	return -1, -1, nil
}

func (r *Repo) UserGetTzOffset(m *tb.Message) (tzOffset int) {
	rows, err := r.db.Query(`
		SELECT "value"
		FROM "bot::userSetting"
		WHERE "tgChatId" = $1 AND "setting" = $2
	`, m.Chat.ID, helpers.USERSET_TZOFF)
	if err != nil {
		LogDbf(r, helpers.ERROR, m, "Encountered error while getting user timezone offset setting: %s", err.Error())
	}
	defer rows.Close()

	if rows.Next() {
		err = rows.Scan(&tzOffset)
		if err != nil {
			LogDbf(r, helpers.ERROR, m, "Encountered error while scanning user timezone offset setting into var: %s", err.Error())
		}
	}
	return
}

func (r *Repo) UserSetTzOffset(m *tb.Message, timezoneOffset int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("could not create db tx for setting timezone offset: %s", err.Error())
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM "bot::userSetting" WHERE "tgChatId" = $1 AND "setting" = $2`, m.Chat.ID, helpers.USERSET_TZOFF)
	if err != nil {
		return fmt.Errorf("could not delete setting timezone offset: %s", err.Error())
	}
	if timezoneOffset != 0 {
		_, err = tx.Exec(`INSERT INTO "bot::userSetting" ("tgChatId", "setting", "value") VALUES ($1, $2, $3)`, m.Chat.ID, helpers.USERSET_TZOFF, timezoneOffset)
		if err != nil {
			return fmt.Errorf("could not insert setting timezone offset: %s", err.Error())
		}
	}

	tx.Commit()
	return nil
}

/**
UserSetNotificationSetting sets user's notification settings.
If daysDelay is < 0, schedule will be disabled.
*/
func (r *Repo) UserSetNotificationSetting(m *tb.Message, daysDelay, hour int) error {
	_, err := r.db.Exec(`DELETE FROM "bot::notificationSchedule" WHERE "tgChatId" = $1;`, m.Chat.ID)
	if daysDelay >= 0 && err == nil { // Condition to enable schedule
		_, err = r.db.Exec(`INSERT INTO "bot::notificationSchedule" ("tgChatId", "delayHours", "notificationHour")
			VALUES ($1, $2, $3);`, m.Chat.ID, daysDelay*24, hour)
	}
	if err != nil {
		return fmt.Errorf("error while setting user notifications schedule: %s", err.Error())
	}
	return nil
}

func (r *Repo) GetUsersToNotify() (*sql.Rows, error) {
	return r.db.Query(`
	SELECT overdue."tgChatId", overdue."count" overdue, COUNT(tx2.*) "allTx"
	FROM
		(
			SELECT DISTINCT u."tgChatId", COUNT(tx.id)
			FROM "auth::user" u, "bot::notificationSchedule" s, "bot::transaction" tx
			WHERE u."tgChatId" = s."tgChatId" AND s."tgChatId" = tx."tgChatId"
				AND tx.archived = FALSE
				AND tx.created + INTERVAL '1 hour' * s."delayHours" <= NOW()
				AND s."notificationHour" = $1
			GROUP BY u."tgChatId"
		) AS overdue,
	  	"bot::transaction" tx2
	WHERE tx2."tgChatId" = overdue."tgChatId"
	GROUP BY overdue."tgChatId", overdue."count"
	`, time.Now().Hour())
}
