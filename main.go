package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	errors "github.com/AustinMCrane/errorutil"
	"github.com/AustinMCrane/tcg-market-watch-api/pkg/store"
	"github.com/AustinMCrane/tcgplayer"
)

var (
	dbHost      = flag.String("db-host", "localhost", "database host")
	dbPort      = flag.String("db-port", "5432", "database port")
	dbUser      = flag.String("db-user", "postgres", "database user")
	dbPassword  = flag.String("db-password", "password", "database password")
	dbName      = flag.String("db-name", "tcg-market-watch-api", "database name")
	ingestPrice = flag.Bool("ingest-price", false, "should just ingest pricing")

	publicKey  = flag.String("public-key", "", "public tcgplayer api key")
	privateKey = flag.String("private-key", "", "private tcgplayer api key")

	defaultRarityName = "Unconfirmed"

	// rarityNameCommon is the name of the common rarity it is not just called
	// Common
	rarityNameCommon = "Common / Short Print"
)

func main() {
	flag.Parse()
	if err := Exec(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

//go:generate mockgen -destination mock_main_test.go -package main -source main.go Tcgplayer
type Tcgplayer interface {
	GetGroups(tcgplayer.GroupParams) ([]*tcgplayer.Group, error)
	GetRarities(params *tcgplayer.RarityParams) ([]*tcgplayer.Rarity, error)
	GetPrinting(params tcgplayer.PrintingParams) ([]*tcgplayer.Printing, error)
	GetConditions(params *tcgplayer.ConditionParams) ([]*tcgplayer.Condition, error)
	GetLanguages(params *tcgplayer.LanguageParams) ([]*tcgplayer.Language, error)
	ListAllProducts(params tcgplayer.ProductParams) ([]*tcgplayer.Product, error)
	ListProductSKUs(skuID int) ([]*tcgplayer.SKU, error)
	GetSKUPrices(skus []int) ([]*tcgplayer.SKUMarketPrice, error)
}

func Exec() error {
	dbConn, err := getDBConnection(*dbHost, *dbPort, *dbUser, *dbPassword, *dbName)
	if err != nil {
		return errors.Wrap(err)
	}

	client, err := tcgplayer.New(*publicKey, *privateKey)
	if err != nil {
		return errors.Wrap(err)
	}

	if *ingestPrice == false {
		err = updateImmutableDataTcgPlayer(dbConn, client)
		if err != nil {
			return errors.Wrap(err)
		}
		return nil
	}

	err = ingetPrices(dbConn, client, time.Millisecond*100)
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func ingetPrices(dbConn *gorm.DB, client Tcgplayer, sleepDuration time.Duration) error {
	skus := []store.SKU{}
	err := dbConn.Select("tcgplayer_id").Find(&skus).Error
	if err != nil {
		return errors.Wrap(err)
	}

	skuGroups := [][]int{}
	currentGroup := []int{}
	count := 0
	maxCount := 100
	for _, s := range skus {
		if count < maxCount {
			currentGroup = append(currentGroup, s.TCGPlayerID)
			count++

		} else {
			skuGroups = append(skuGroups, currentGroup)
			currentGroup = []int{}
			count = 0
		}
	}

	if len(currentGroup) > 0 {
		skuGroups = append(skuGroups, currentGroup)
	}

	for _, skuGroup := range skuGroups {
		prices, err := client.GetSKUPrices(skuGroup)
		if err != nil {
			return errors.Wrap(err)
		}

		// insert tcgplayer prices
		pricesToCreate := []store.SKUPrice{}
		for _, p := range prices {
			pricesToCreate = append(pricesToCreate, store.SKUPrice{
				SKUID:    p.SKUID,
				Price:    float32(p.LowPrice),
				Shipping: float32(p.LowestShipping),
			})
		}

		err = dbConn.Create(&pricesToCreate).Error
		if err != nil {
			return errors.Wrap(err)
		}
		time.Sleep(sleepDuration)
	}
	return nil
}

func updateImmutableDataTcgPlayer(dbConn *gorm.DB, client Tcgplayer) error {
	// check if groups changed from tcgplayer
	groups, err := getGroups(client)
	if err != nil {
		return errors.Wrap(err)
	}

	currentGroupIDs := []int{}
	err = dbConn.Model(&store.Group{}).Select("tcgplayer_id").
		Find(&currentGroupIDs).Error
	if err != nil {
		return errors.Wrap(err)
	}

	needUpdate := false
	for _, g := range groups {
		for _, id := range currentGroupIDs {
			if id == g.ID {
				needUpdate = true
				break
			}
		}
	}

	if needUpdate == true {
		err := dropData(dbConn)
		if err != nil {
			return errors.Wrap(err)
		}
	} else if len(currentGroupIDs) > 0 {
		log.Println("data already exists")
		return nil
	}

	createdGroups, err := syncGroups(dbConn, groups)
	if err != nil {
		return errors.Wrap(err)
	}

	rarities, err := getRarities(client)
	if err != nil {
		return errors.Wrap(err)
	}

	createdRarities, err := syncRarities(dbConn, rarities)
	if err != nil {
		return errors.Wrap(err)
	}

	printings, err := getPrintings(client)
	if err != nil {
		return errors.Wrap(err)
	}

	createdPrintings, err := syncPrintings(dbConn, printings)
	if err != nil {
		return errors.Wrap(err)
	}

	conditions, err := getConditions(client)
	if err != nil {
		return errors.Wrap(err)
	}

	createdConditions, err := syncConditions(dbConn, conditions)
	if err != nil {
		return errors.Wrap(err)
	}

	languages, err := getLanguages(client)
	if err != nil {
		return errors.Wrap(err)
	}

	createdLanguages, err := syncLanguages(dbConn, languages)
	if err != nil {
		return errors.Wrap(err)
	}

	products, err := getProducts(client, time.Millisecond*200)
	if err != nil {
		return errors.Wrap(err)
	}

	createdProducts, err := syncProducts(dbConn, createdGroups, createdRarities, products)
	if err != nil {
		return errors.Wrap(err)
	}

	err = syncSKUs(dbConn, createdLanguages, createdConditions, createdPrintings, createdProducts, products)
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func syncSKUs(dbConn *gorm.DB, languages []*store.Language, conditions []*store.Condition,
	printings []*store.Printing, products []*store.Product, productsTCG []*tcgplayer.Product) error {
	p := []*store.SKU{}
	for _, prod := range productsTCG {
		for _, s := range prod.SKUS {
			productID := 0
			printingID := 0
			conditionID := 0
			languageID := 0

			// BUG: this is always coming up with 1 for the values here
			for _, p := range products {
				if p.TCGPlayerID == s.ProductID {
					productID = p.ID
				}
			}
			for _, d := range printings {
				if d.TCGPlayerID == s.PrintingID {
					printingID = d.ID
				}
			}
			for _, c := range conditions {
				if c.TCGPlayerID == s.ConditionID {
					conditionID = c.ID
				}
			}
			isEnglish := false
			for _, l := range languages {
				if l.TCGPlayerID == s.LanguageID {
					languageID = l.ID

					// english is always 1
					isEnglish = l.TCGPlayerID == 1
				}
			}
			// NOTE: only care about english
			if !isEnglish {
				break
			}

			group := store.SKU{
				TCGPlayerID: s.SKUID,
				ProductID:   productID,
				PrintingID:  printingID,
				ConditionID: conditionID,
				LanguageID:  languageID,
			}
			p = append(p, &group)
		}
	}

	err := dbConn.CreateInBatches(&p, 3000).Error
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func syncGroups(dbConn *gorm.DB, groups []*tcgplayer.Group) ([]*store.Group, error) {
	p := []*store.Group{}
	for _, g := range groups {
		group := store.Group{
			Name:        g.Name,
			TCGPlayerID: g.ID,
		}
		p = append(p, &group)
	}

	err := dbConn.Create(&p).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return p, nil
}

func syncConditions(dbConn *gorm.DB, conditions []*tcgplayer.Condition) ([]*store.Condition, error) {
	p := []*store.Condition{}
	for _, g := range conditions {
		condition := store.Condition{
			Name:         g.Name,
			Abbreviation: g.Abbreviation,
			TCGPlayerID:  g.ID,
		}
		p = append(p, &condition)
	}

	err := dbConn.Create(&p).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return p, nil
}

func syncLanguages(dbConn *gorm.DB, languages []*tcgplayer.Language) ([]*store.Language, error) {
	p := []*store.Language{}
	for _, g := range languages {
		language := store.Language{
			Name:        g.Name,
			TCGPlayerID: g.ID,
		}
		p = append(p, &language)
	}

	err := dbConn.Create(&p).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return p, nil
}
func syncPrintings(dbConn *gorm.DB, printings []*tcgplayer.Printing) ([]*store.Printing, error) {
	p := []*store.Printing{}
	for _, g := range printings {
		printing := store.Printing{
			Name:        g.Name,
			TCGPlayerID: g.ID,
		}
		p = append(p, &printing)
	}

	err := dbConn.Create(&p).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return p, nil
}

func syncRarities(dbConn *gorm.DB, rarities []*tcgplayer.Rarity) ([]*store.Rarity, error) {
	p := []*store.Rarity{}
	for _, r := range rarities {
		rarity := store.Rarity{
			Name:        r.Name,
			TCGPlayerID: r.ID,
		}
		p = append(p, &rarity)
	}

	err := dbConn.Create(&p).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return p, nil
}

func syncProducts(dbConn *gorm.DB, groups []*store.Group, rarities []*store.Rarity,
	products []*tcgplayer.Product) ([]*store.Product, error) {
	a := []*store.Product{}
	details := []*store.Detail{}
	for _, p := range products {
		// check if name exists in detailNames
		found := false
		for _, n := range details {
			if n.Name == p.CleanName {
				found = true
				continue
			}
		}
		if !found {
			details = append(details, &store.Detail{Name: p.CleanName})
		}
	}

	createdDetails, err := syncDetails(dbConn, details)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	var defaultRarity store.Rarity
	err = dbConn.Where("name = ?", "Unconfirmed").First(&defaultRarity).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}
	commonRarity := store.Rarity{}
	err = dbConn.Where("name = ?", rarityNameCommon).
		First(&commonRarity).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	for _, p := range products {
		groupID := 0
		rarityID := 0
		for _, g := range groups {
			if g.TCGPlayerID == p.GroupID {
				groupID = g.ID
				break
			}
		}

		rare, err := p.GetExtendedData("Rarity")
		if err != nil {
			log.Println("unable to find rarity for product: ", p.Name)
			rarityID = defaultRarity.ID
		} else {
			found := false

			// NOTE: there seems to be cards that have a rarity of "Common" instead of "Common / Short Print"
			if rare.Value == "Common" {
				rarityID = commonRarity.ID
			} else {
				for _, r := range rarities {
					if r.Name == rare.Value {
						rarityID = r.ID
						found = true
						break
					}
				}

				if !found {
					rarityID = defaultRarity.ID
				}
			}
		}

		detailID := 0
		for _, d := range createdDetails {
			if d.Name == p.CleanName {
				detailID = d.ID
				break
			}
		}

		if rarityID == 0 {
			rarityID = defaultRarity.ID
		}

		product := store.Product{
			DetailID:     detailID,
			GroupID:      groupID,
			RarityID:     rarityID,
			ImageURL:     p.ImageURL,
			TCGPlayerID:  p.ID,
			TCGPlayerURL: p.URL,
		}
		a = append(a, &product)
	}

	err = dbConn.CreateInBatches(&a, 1000).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return a, nil
}

func syncDetails(dbConn *gorm.DB, details []*store.Detail) ([]*store.Detail, error) {
	// create each detail if it doesn't exist
	createdDetails := []*store.Detail{}
	for _, d := range details {
		found := false
		for _, c := range createdDetails {
			if c.Name == d.Name {
				found = true
				continue
			}
		}

		if !found {
			createdDetails = append(createdDetails, d)
		}
	}

	err := dbConn.CreateInBatches(&createdDetails, 1000).Error
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return createdDetails, nil
}

func getGroups(client Tcgplayer) ([]*tcgplayer.Group, error) {
	limit := 100
	page := 0
	groups := []*tcgplayer.Group{}
	for {
		params := tcgplayer.GroupParams{
			CategoryID: tcgplayer.CategoryYugioh,
			Limit:      limit,
			Offset:     limit * page,
		}

		p, err := client.GetGroups(params)
		if err != nil {
			return nil, errors.Wrap(err)
		}
		groups = append(groups, p...)
		if len(p) < limit {
			return groups, nil
		}

		page++
	}
}

func getProducts(client Tcgplayer, sleepDuration time.Duration) ([]*tcgplayer.Product, error) {
	limit := 100
	page := 0
	products := []*tcgplayer.Product{}
	for {
		params := tcgplayer.ProductParams{
			CategoryID: tcgplayer.CategoryYugioh,
			Limit:      limit,
			Offset:     limit * page,
		}

		p, err := client.ListAllProducts(params)
		if err != nil {
			return nil, errors.Wrap(err)
		}
		products = append(products, p...)
		if len(p) < limit {
			return products, nil
		}

		page++
		time.Sleep(sleepDuration)
		log.Println("PAGE:", page)
	}
}

func getRarities(client Tcgplayer) ([]*tcgplayer.Rarity, error) {
	params := &tcgplayer.RarityParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}

	rarities, err := client.GetRarities(params)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return rarities, nil
}

func getConditions(client Tcgplayer) ([]*tcgplayer.Condition, error) {
	params := &tcgplayer.ConditionParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}

	conditions, err := client.GetConditions(params)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return conditions, nil
}

func getLanguages(client Tcgplayer) ([]*tcgplayer.Language, error) {
	params := &tcgplayer.LanguageParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}

	languages, err := client.GetLanguages(params)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return languages, nil
}

func getPrintings(client Tcgplayer) ([]*tcgplayer.Printing, error) {
	params := tcgplayer.PrintingParams{
		CategoryID: tcgplayer.CategoryYugioh,
	}

	printings, err := client.GetPrinting(params)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return printings, nil
}

func getDBConnection(dbHost string, dbPort string, dbUser string, dbPassword string, dbName string) (*gorm.DB, error) {
	postgresqlDbInfo := fmt.Sprintf("host=%s port=%s user=%s "+
		"password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := gorm.Open(postgres.Open(postgresqlDbInfo), &gorm.Config{})
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return db, nil
}

func dropData(dbConn *gorm.DB) error {
	// truncate all tables
	err := dbConn.Exec("TRUNCATE TABLE products, details, groups, rarities, conditions, languages, printings CASCADE").Error
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func getDetailID(dbConn *gorm.DB, name string) (int, error) {
	var detail store.Detail
	err := dbConn.Where("name = ?", name).First(&detail).Error
	if err != nil {
		return 0, errors.Wrap(err)
	}

	return detail.ID, nil
}
