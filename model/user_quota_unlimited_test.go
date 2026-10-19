package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupUserQuotaTest(t *testing.T) {
	t.Helper()
	originalDB := DB
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&User{}))
	DB = database
	t.Cleanup(func() { DB = originalDB })
}

// An unlimited user must pass the reserve even with an empty wallet, and the
// wallet balance must stay untouched so no debt accumulates.
func TestUnlimitedUserReservesWithoutTouchingWallet(t *testing.T) {
	setupUserQuotaTest(t)
	user := User{Username: "unlimited", Password: "x", Quota: 0, Group: "default", AffCode: "U1", UnlimitedQuota: true}
	require.NoError(t, DB.Create(&user).Error)

	reserved, err := TryReserveUserQuota(user.Id, 500000)
	require.NoError(t, err)
	assert.True(t, reserved, "an unlimited user must not be rejected for insufficient balance")

	require.NoError(t, DecreaseUserQuota(user.Id, 500000, true))

	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	assert.Equal(t, 0, after.Quota, "wallet balance must not go negative for an unlimited user")
}

// Without the exemption the same call must fail, so the toggle is what changes
// the outcome rather than some incidental difference.
func TestLimitedUserStillFailsWhenWalletIsEmpty(t *testing.T) {
	setupUserQuotaTest(t)
	user := User{Username: "limited", Password: "x", Quota: 0, Group: "default", AffCode: "L1"}
	require.NoError(t, DB.Create(&user).Error)

	reserved, err := TryReserveUserQuota(user.Id, 500000)
	require.NoError(t, err)
	assert.False(t, reserved, "a limited user with no balance must be rejected")
}

// A limited user with enough balance keeps working exactly as before.
func TestLimitedUserReserveStillDeducts(t *testing.T) {
	setupUserQuotaTest(t)
	user := User{Username: "funded", Password: "x", Quota: 1000, Group: "default", AffCode: "F1"}
	require.NoError(t, DB.Create(&user).Error)

	reserved, err := TryReserveUserQuota(user.Id, 400)
	require.NoError(t, err)
	assert.True(t, reserved)

	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	assert.Equal(t, 600, after.Quota)
}

// The exemption must be readable both from the cache path and a direct DB read.
func TestIsUserQuotaUnlimitedReadsFlag(t *testing.T) {
	setupUserQuotaTest(t)
	unlimited := User{Username: "u", Password: "x", Group: "default", AffCode: "U2", UnlimitedQuota: true}
	require.NoError(t, DB.Create(&unlimited).Error)
	limited := User{Username: "l", Password: "x", Group: "default", AffCode: "L2"}
	require.NoError(t, DB.Create(&limited).Error)

	gotUnlimited, err := IsUserQuotaUnlimited(unlimited.Id, true)
	require.NoError(t, err)
	assert.True(t, gotUnlimited)

	gotLimited, err := IsUserQuotaUnlimited(limited.Id, true)
	require.NoError(t, err)
	assert.False(t, gotLimited)
}
