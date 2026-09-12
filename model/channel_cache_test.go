/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInitChannelCacheAppliesModelPrefix(t *testing.T) {
	originalDB := DB
	originalMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCache
		InitChannelCache()
	})

	var err error
	DB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, DB.AutoMigrate(&Ability{}, &Channel{}))

	channel := &Channel{
		Id:            1,
		Models:        "glm-5.3-flash",
		Group:         "default",
		Status:        common.ChannelStatusEnabled,
		OtherSettings: `{"model_prefix":"zai-v4"}`,
	}
	require.NoError(t, channel.Insert())

	common.MemoryCacheEnabled = true
	InitChannelCache()

	// The client-facing prefixed name must route to the channel through the
	// in-memory routing index, matching the abilities table (DB path).
	selected, err := GetRandomSatisfiedChannel("default", "zai-v4/glm-5.3-flash", 0, nil)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 1, selected.Id)

	// The bare upstream name is not served: abilities only store the prefixed
	// name, so the routing index must not advertise the raw name either.
	selected, err = GetRandomSatisfiedChannel("default", "glm-5.3-flash", 0, nil)
	require.NoError(t, err)
	assert.Nil(t, selected)
}