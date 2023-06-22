package main

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/AustinMCrane/tcg-market-watch-api/pkg/store"
	"github.com/AustinMCrane/tcgplayer"
	"github.com/DATA-DOG/go-sqlmock"
	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func GetMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	var (
		db  *sql.DB
		err error
	)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	dbConn, err := gorm.Open(postgres.New(postgres.Config{
		Conn:       db,
		DriverName: "postgres",
	}), &gorm.Config{})
	require.NoError(t, err)

	return dbConn, mock
}

func TestGetGroups(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	client := NewMockTcgplayer(ctrl)

	client.EXPECT().GetGroups(tcgplayer.GroupParams{
		CategoryID: tcgplayer.CategoryYugioh,
		Limit:      100,
		Offset:     0,
	}).Return([]*tcgplayer.Group{{Name: "test-group"}}, nil)

	groups, err := getGroups(client, tcgplayer.CategoryYugioh)
	require.NoError(t, err)
	require.Len(t, groups, 1)
}

func TestSyncDetails(t *testing.T) {
	dbConn, mock := GetMockDB(t)

	detail := []*store.Detail{{Name: "test"}}
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO \"details\" (.+) RETURNING \"id\"").
		WithArgs("test").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(1))
	mock.ExpectCommit()

	created, err := syncDetails(dbConn, detail)
	require.NoError(t, err)
	require.Len(t, created, len(detail))
}

func TestSyncGroups(t *testing.T) {
	dbConn, mock := GetMockDB(t)
	groups := []*tcgplayer.Group{
		{
			ID:   1,
			Name: "test-1",
		},
		{
			ID:   2,
			Name: "test-2",
		},
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"groups\" (.+) VALUES (.+)`).
		WithArgs("test-1", 1, "test-2", 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))
	mock.ExpectCommit()

	created, err := syncGroups(dbConn, groups)
	require.NoError(t, err)
	require.Len(t, created, len(groups))
}

func TestUpdateImmutableDataTcgPlayer(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := NewMockTcgplayer(ctrl)
	dbConn, mock := GetMockDB(t)

	tcgGroups := []*tcgplayer.Group{
		{
			ID:         1,
			CategoryID: tcgplayer.CategoryYugioh,
			Name:       "test-1",
		},
		{
			ID:         2,
			CategoryID: tcgplayer.CategoryYugioh,
			Name:       "test-2",
		},
	}

	rarities := []*tcgplayer.Rarity{
		{
			ID:   1,
			Name: "Common",
		},
	}

	printings := []*tcgplayer.Printing{
		{
			ID:   1,
			Name: "1st Edition",
		},
	}

	conditions := []*tcgplayer.Condition{
		{
			ID:           1,
			Name:         "Near Mint",
			Abbreviation: "NM",
		},
	}

	languages := []*tcgplayer.Language{
		{
			ID:           1,
			Name:         "English",
			Abbreviation: "EN",
		},
	}

	products := []*tcgplayer.Product{
		{
			ID:         1,
			CategoryID: tcgplayer.CategoryYugioh,
			GroupID:    1,
			Name:       "test",
			CleanName:  "test-name",
			ImageURL:   "test-image-url",
			URL:        "test-url",
			ExtendedData: []tcgplayer.ExtendedData{
				{
					Name:  "Rarity",
					Value: "Common",
				},
			},
			SKUS: []tcgplayer.SKU{
				{
					SKUID:       1,
					ProductID:   1,
					PrintingID:  1,
					LanguageID:  1,
					ConditionID: 1,
				},
			},
		},
	}

	mock.ExpectQuery(`SELECT \"tcgplayer_id\" FROM \"groups\"`).WillReturnRows(sqlmock.NewRows([]string{"tcgplayer_id"}).
		AddRow(1))

	// truncate the tables
	mock.ExpectExec(`TRUNCATE TABLE products, details, groups, rarities, conditions, languages, printings CASCADE`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Get Groups and Sync
	client.EXPECT().GetGroups(tcgplayer.GroupParams{
		CategoryID: tcgplayer.CategoryYugioh,
		Limit:      100,
		Offset:     0,
	}).Return(tcgGroups, nil)

	// inserts the groups
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"groups\" (.+)`).
		WithArgs("test-1", 1, "test-2", 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(1).
			AddRow(2))
	mock.ExpectCommit()

	// Get rarities and sync
	client.EXPECT().GetRarities(&tcgplayer.RarityParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}).Return(rarities, nil)

	// insert the rarities
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"rarities\" (.+)`).
		WithArgs("Common", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	// Get printings and sync
	client.EXPECT().GetPrinting(tcgplayer.PrintingParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}).Return(printings, nil)

	// insert the printings
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"printings\" (.+)`).
		WithArgs("1st Edition", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	// Get conditions and sync
	client.EXPECT().GetConditions(&tcgplayer.ConditionParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}).Return(conditions, nil)

	// insert the conditions
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"conditions\" (.+)`).
		WithArgs("Near Mint", "NM", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	// Get languages and sync
	client.EXPECT().GetLanguages(&tcgplayer.LanguageParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}).Return(languages, nil)

	// insert the conditions
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"languages\" (.+)`).
		WithArgs("English", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	// Get products and sync
	client.EXPECT().ListAllProducts(tcgplayer.ProductParams{
		CategoryID: tcgplayer.CategoryYugioh,
		Limit:      100,
		Offset:     0,
	}).Return(products, nil)

	mock.ExpectBegin()
	mock.ExpectQuery("(.+)").
		WithArgs("test-name").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT (.+) FROM \"rarities\"`).
		WithArgs(defaultRarityName).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	mock.ExpectQuery(`SELECT (.+) FROM \"rarities\"`).
		WithArgs(rarityNameCommon).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	// insert the products
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"products\" (.+)`).
		WithArgs(tcgplayer.CategoryYugioh, 1, 1, 1, "test-image-url", 1, "test-url").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	// insert the skus
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO \"skus\" (.+)`).
		WithArgs(1, 1, 1, 1, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	err := updateImmutableDataTcgPlayer(dbConn, client, tcgplayer.CategoryYugioh)
	require.NoError(t, err)
}

func TestIngestPrice(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := NewMockTcgplayer(ctrl)
	dbConn, mock := GetMockDB(t)

	skuID := 1
	price := float32(1.0)
	shipping := float32(0.1)

	mock.ExpectQuery("SELECT \"tcgplayer_id\" FROM \"skus\"").
		WillReturnRows(sqlmock.NewRows([]string{"tcgplayer_id"}).AddRow(1))
	client.EXPECT().GetSKUPrices([]int{skuID}).
		Return([]*tcgplayer.SKUMarketPrice{
			{SKUID: skuID, LowPrice: 1.0, LowestShipping: 0.1},
		}, nil)

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO \"sku_prices\" (.+)").
		WithArgs(skuID, price, shipping).WillReturnRows(sqlmock.NewRows([]string{"ingested_at", "id"}).AddRow(time.Now(), 1))
	mock.ExpectCommit()

	err := ingetPrices(dbConn, client, 0)
	require.NoError(t, err)

}

func TestIngestPrice_ClientError(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := NewMockTcgplayer(ctrl)
	dbConn, mock := GetMockDB(t)

	skuID := 1

	mock.ExpectQuery("SELECT \"tcgplayer_id\" FROM \"skus\"").
		WillReturnRows(sqlmock.NewRows([]string{"tcgplayer_id"}).AddRow(skuID))
	client.EXPECT().GetSKUPrices([]int{skuID}).
		Return(nil, errors.New("unable to get prices"))

	err := ingetPrices(dbConn, client, 0)
	require.Error(t, err)
}
