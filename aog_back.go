package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/restream/reindexer"
)

var db *reindexer.Reindexer

func QueryMediaItems(fullTextQ string) (ret []MediaItem) {

	q := db.Query("media_items").Match("search", "@name^1 "+fullTextQ).Limit(10)

	items, err := q.Exec().FetchAll()

	if err != nil {
		log.Fatal(err)
	}

	for _, mi := range items {
		ret = append(ret, *mi.(*MediaItem))
	}
	return
}

func lookupMediaItemsByGenre(genre string) *MediaItem {
	q := db.Query("media_items").
		Where("genres_names", reindexer.EQ, genre).
		Where("type", reindexer.EQ, "film").
		Sort("imdb", true).
		Limit(100)

	items, err := q.Exec().FetchAll()

	if err != nil {
		log.Println(err.Error())
	}

	return items[rand.Int()%len(items)].(*MediaItem)

}

func QueryEPGItems(fullTextQ string) (ret []EPGItem) {

	q := db.Query("epg").
		Match("search", "@name^1,description^0.3 "+fullTextQ).
		Where("start_time", reindexer.GT, time.Now().Unix()).
		Limit(10)

	items, err := q.Exec().FetchAll()

	if err != nil {
		log.Fatal(err)
	}

	for _, mi := range items {
		ret = append(ret, *mi.(*EPGItem))
	}
	return
}

func AOGHandler(c echo.Context) error {

	dec := json.NewDecoder(c.Request().Body)
	params := AOGRequestParams{}

	if err := dec.Decode(&params); err != nil {
		fmt.Println(err)
		return c.String(400, "Bad request\n")
	}

	fmt.Printf("p1->%#v\n\n\n", params.QueryResult.Parameters)
	fmt.Printf("p2->%#v\n\n\n", params.QueryResult.OutputContexts[0].Parameters)

	textToSpeech := "Я хз что ответить )"

	paramGenre := params.QueryResult.Parameters["movie-genre"]

	if paramGenre != "" {
		mi := lookupMediaItemsByGenre(paramGenre)
		textToSpeech = fmt.Sprintf("Рекомендую посмотреть %s с %s %d года", mi.Name, mi.Persons[0].Name, mi.Year)
	}

	return sendAOGResponce(c, textToSpeech)

}

func sendAOGResponce(c echo.Context, testToSpeech string) error {
	ans := AOGRequestAnswer{}

	ans.Payload.Google.ExpectUserResponse = true
	ans.Payload.Google.RichResponse.Items = append(ans.Payload.Google.RichResponse.Items,
		RespItem{
			SimpleResponse: SimpleResponse{TextToSpeech: testToSpeech},
		},
	)

	return c.JSON(200, ans)
}

func main() {
	initDB()
	// fmt.Println(QueryMediaItems("терминатор"))
	// fmt.Println(QueryEPGItems("новости"))

	log.Println("Staring AOG server")

	e := echo.New()
	e.Use(middleware.Logger())
	e.POST("/handler", AOGHandler)

	e.Logger.Fatal(e.Start(":7777"))
}

// Bullshit

type MediaItem struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	ShortDescription string `json:"short_description"`
	Year             string `json:"year"`
	Genres           []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Coutries []string `json:"countries"`
	Persons  []struct {
		Name string `json:"name"`
	} `json:"persons"`
}

type EPGItem struct {
	Name        string `json:"name"`
	Description string `json:"descrition"`
	ChannelID   int    `json:"channel_id"`
	StartTime   int    `json:"start_time"`
	EndTime     int    `json:"end_time"`
}

func initDB() {
	db = reindexer.NewReindex("cproto://reindexer.org:6534/itv_api_ng")

	if err := db.OpenNamespace("media_items", reindexer.DefaultNamespaceOptions(), &MediaItem{}); err != nil {
		log.Fatal(err)
	}
	if err := db.OpenNamespace("epg", reindexer.DefaultNamespaceOptions(), &EPGItem{}); err != nil {
		log.Fatal(err)
	}
}

type AOGRequestParams struct {
	ResponseID  string `json:"responseId"`
	Session     string `json:"session"`
	QueryResult struct {
		QueryText                string            `json:"queryText"`
		Parameters               map[string]string `json:"parameters"`
		AllRequiredParamsPresent bool              `json:"allRequiredParamsPresent"`
		FulfillmentText          string            `json:"fulfillmentText"`
		FulfillmentMessages      []struct {
			Text struct {
				Text []string `json:"text"`
			} `json:"text"`
		} `json:"fulfillmentMessages"`
		OutputContexts []struct {
			Name          string            `json:"name"`
			LifespanCount float32           `json:"lifespanCount"`
			Parameters    map[string]string `json:"parameters"`
		} `json:"outputContexts"`
		Intent struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"intent"`
		IntentDetectionConfidence float32 `json:"intentDetectionConfidence"`
		DiagnosticInfo            struct {
		} `json:"diagnosticInfo"`
		LanguageCode string `json:"languageCode"`
	} `json:"queryResult"`
	OriginalDetectIntentRequest struct {
	} `json:"originalDetectIntentRequest"`
}

type RespItem struct {
	SimpleResponse SimpleResponse `json:"simpleResponse"`
}

type SimpleResponse struct {
	TextToSpeech string `json:"textToSpeech"`
}

type AOGRequestAnswer struct {
	// FulfillmentText     string `json:"fulfillmentText"`
	// FulfillmentMessages []struct {
	// 	Card struct {
	// 		Title    string `json:"title"`
	// 		Subtitle string `json:"subtitle"`
	// 		ImageURI string `json:"imageUri"`
	// 		Buttons  []struct {
	// 			Text     string `json:"text"`
	// 			Postback string `json:"postback"`
	// 		} `json:"buttons"`
	// 	} `json:"card"`
	// } `json:"fulfillmentMessages"`
	// Source  string `json:"source"`
	Payload struct {
		Google struct {
			ExpectUserResponse bool `json:"expectUserResponse"`
			RichResponse       struct {
				Items []RespItem `json:"items"`
			} `json:"richResponse"`
		} `json:"google"`
		// Facebook struct {
		// 	Text string `json:"text"`
		// } `json:"facebook"`
		// Slack struct {
		// 	Text string `json:"text"`
		// } `json:"slack"`
	} `json:"payload"`
	// OutputContexts []struct {
	// 	Name          string            `json:"name"`
	// 	LifespanCount int               `json:"lifespanCount"`
	// 	Parameters    map[string]string `json:"parameters"`
	// } `json:"outputContexts"`
	// FollowupEventInput struct {
	// 	Name         string            `json:"name"`
	// 	LanguageCode string            `json:"languageCode"`
	// 	Parameters   map[string]string `json:"parameters"`
	// } `json:"followupEventInput"`
}
