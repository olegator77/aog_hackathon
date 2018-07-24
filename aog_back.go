package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"reflect"
	"strings"
	"time"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/restream/reindexer"
)

var db *reindexer.Reindexer
var sessionParams = make(map[string]map[string]interface{})

func addToSessionParams(session string, new map[string]interface{}) {
	params, ok := sessionParams[session]
	if !ok {
		sessionParams[session] = new
	}

	for key, _ := range params {
		if newValue, ok := new[key]; ok && newValue != nil {
			if reflect.DeepEqual(newValue, reflect.Zero(reflect.TypeOf(newValue)).Interface()) {
				continue
			}

			params[key] = newValue
		}
	}
}

func getParamFromSession(session, paramKey string) interface{} {
	params, ok := sessionParams[session]
	if !ok {
		return ""
	}

	param, ok := params[paramKey]
	if !ok {
		return ""
	}

	return param
}

func addPeriodQuery(q *reindexer.Query, period map[string]interface{}) {
	if startDate, ok := period["startDate"]; ok {
		if sd, ok := startDate.(string); ok {
			sdTime, err := time.Parse(time.RFC3339, sd)
			if err != nil {
				fmt.Println(err)
			} else {
				q = q.Where("year", reindexer.GE, sdTime.Year())
			}
		}
	}

	if endDate, ok := period["endDate"]; ok {
		if sd, ok := endDate.(string); ok {
			sdTime, err := time.Parse(time.RFC3339, sd)
			if err != nil {
				fmt.Println(err)
			} else {
				q = q.Where("year", reindexer.LE, sdTime.Year())
			}
		}
	}
}

func lookupMediaItems(genre, countries, period, persons, name interface{}, queryText string) *MediaItem {
	sortByRelevancy := false
	q := db.Query("media_items").
		Where("type", reindexer.EQ, "film").
		Where("parent_id", reindexer.EMPTY, 0).
		Limit(100)

	// https://github.com/Restream/reindexer/blob/master/fulltext.md#text-query-format
	ftDSL := ""

	if g, ok := genre.(string); ok && g != "" {
		// q = q.Where("genres_names", reindexer.EQ, genre)
		ftDSL = ftDSL + "@genres_names +" + genre.(string) + "~ "
	}

	if o, ok := countries.(string); ok && o != "" {
		// q = q.Where("countries", reindexer.EQ, origin)
		ftDSL = ftDSL + "@countries +" + countries.(string) + "~ "
	}

	if o, ok := persons.(string); ok && o != "" {
		// q = q.Where("actors", reindexer.EQ, origin)
		ftDSL = ftDSL + "@persons_names +" + persons.(string) + "~ "
	}

	if o, ok := name.(string); ok && o != "" {
		// For name full text queries keep relevancy sorting
		ftDSL = ftDSL + "@name +" + name.(string) + "~ "
		sortByRelevancy = true
	}

	if ftDSL == "" {
		ftDSL = "@*^0.3,name^1.1 " + queryText
		sortByRelevancy = true
	}

	if !sortByRelevancy {
		q.Sort("imdb", true)
	}

	q.Match("search", ftDSL)

	if p, ok := period.(map[string]interface{}); ok {
		addPeriodQuery(q, p)
	}

	items, err := q.Exec().FetchAll()
	if err != nil {
		log.Println(err.Error())
	}

	if len(items) == 0 {
		return nil
	}
	if sortByRelevancy {
		return items[0].(*MediaItem)
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

func resetHandler(c echo.Context, session string, params AOGRequestParams) error {
	delete(sessionParams, session)
	return sendAOGResponce(c, "Параметры сброшены", nil, nil)
}

func defaultHandler(c echo.Context, session string, params AOGRequestParams) error {
	addToSessionParams(session, params.QueryResult.Parameters)

	textToSpeech := "К сожалению, ничего не найдено"

	paramGenre := getParamFromSession(session, "movie-genre")
	paramOrigin := getParamFromSession(session, "movie-origin")
	paramDatePeriod := getParamFromSession(session, "date-period")
	paramPersons := getParamFromSession(session, "movie-persons")
	paramName := getParamFromSession(session, "movie-name")

	mi := lookupMediaItems(paramGenre, paramOrigin, paramDatePeriod, paramPersons, paramName, params.QueryResult.QueryText)
	var msg *FulfillmentMessage
	var card *BasicCard

	if mi != nil {
		textToSpeech = fmt.Sprintf("Рекомендую посмотреть %s от %s %s года", mi.Name, mi.Persons[0].Name, mi.Year)

		subTitle := mi.ShortDescription
		if len(subTitle) > 120 {
			subTitle = subTitle[0:120] + "..."
		}

		imageURL := "https://mos-itv01.svc.iptv.rt.ru" + mi.Logo
		openURL := fmt.Sprintf("http://production.smarttv.itv.restr.im/pc/#/media_item/%d", mi.ID)

		msg = &FulfillmentMessage{
			Card: FulfillmentCard{
				ImageURI: imageURL,
				Title:    mi.Name,
				Subtitle: subTitle,
				Buttons: []FulfillmentButton{
					FulfillmentButton{
						"Смотреть",
						openURL,
					},
				},
			},
		}

		card = &BasicCard{
			Title: mi.Name,
			Image: Image{
				URL:               imageURL,
				AccessibilityText: mi.Name,
			},
			Buttons: []Button{
				Button{
					Title: "Смотреть",
					OpenURLAction: OpenURLAction{
						URL: openURL,
					},
				},
			},
			ImageDisplayOptions: "WHITE",
		}
	}

	return sendAOGResponce(c, textToSpeech, msg, card)

}

func sendAOGResponce(c echo.Context, testToSpeech string, msg *FulfillmentMessage, card *BasicCard) error {
	fmt.Printf("textToSpeech = %+v\n", testToSpeech)

	ans := AOGRequestAnswer{}

	ans.Payload.Google.ExpectUserResponse = true
	ans.Payload.Google.RichResponse.Items = append(ans.Payload.Google.RichResponse.Items,
		RespItem{
			SimpleResponse: &SimpleResponse{TextToSpeech: testToSpeech},
		},
	)
	if card != nil {
		ans.Payload.Google.RichResponse.Items = append(ans.Payload.Google.RichResponse.Items,
			RespItem{
				BasicCard: card,
			},
		)
	}

	if msg != nil {
		ans.FulfillmentText = testToSpeech
		ans.FulfillmentMessages = append(ans.FulfillmentMessages, *msg)
	}

	jsn, _ := json.Marshal(ans)
	fmt.Printf("\n\n%s\n\n", string(jsn))

	return c.JSON(200, ans)
}
func AOGHandler(c echo.Context) error {
	dec := json.NewDecoder(c.Request().Body)
	params := AOGRequestParams{}

	if err := dec.Decode(&params); err != nil {
		fmt.Println(err)
		return c.String(400, "Bad request\n")
	}

	parts := strings.Split(params.QueryResult.OutputContexts[0].Name, "/contexts/")
	session := parts[0]

	switch params.QueryResult.Intent.DisplayName {
	case "find-movie - reset":
		return resetHandler(c, session, params)
	default:
		return defaultHandler(c, session, params)
	}
}

func main() {
	initDB()

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
	Logo             string `json:"logo"`
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
	Description string `json:"description"`
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
	go pingDB()
}

func pingDB() {
	for {
		db.Query("media_items").Limit(0).Exec().Close()
		time.Sleep(10 * time.Second)
	}
}

type AOGRequestParams struct {
	ResponseID  string `json:"responseId"`
	Session     string `json:"session"`
	QueryResult struct {
		QueryText                string                 `json:"queryText"`
		Parameters               map[string]interface{} `json:"parameters"`
		AllRequiredParamsPresent bool                   `json:"allRequiredParamsPresent"`
		FulfillmentText          string                 `json:"fulfillmentText,omitempty"`
		FulfillmentMessages      []struct {
			Text struct {
				Text []string `json:"text"`
			} `json:"text"`
		} `json:"fulfillmentMessages,omitempty"`
		OutputContexts []OutputContext `json:"outputContexts"`
		Intent         struct {
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
	SimpleResponse *SimpleResponse `json:"simpleResponse,omitempty"`
	BasicCard      *BasicCard      `json:"bacicCard,omitempty"`
}

type BasicCard struct {
	Title               string   `json:"title"`
	Image               Image    `json:"image"`
	Buttons             []Button `json:"buttons"`
	ImageDisplayOptions string   `json:"imageDisplayOptions"`
}

type Image struct {
	URL               string `json:"url"`
	AccessibilityText string `json:"accessibilityText"`
}

type Button struct {
	Title         string        `json:"title"`
	OpenURLAction OpenURLAction `json:"openUrlAction"`
}

type OpenURLAction struct {
	URL string `json:"url"`
}

type Suggestion struct {
	Title string `json:"title"`
}

type SimpleResponse struct {
	TextToSpeech string `json:"textToSpeech"`
}

type OutputContext struct {
	Name          string                 `json:"name"`
	LifespanCount int                    `json:"lifespanCount"`
	Parameters    map[string]interface{} `json:"parameters"`
}

type FulfillmentButton struct {
	Text     string `json:"text"`
	Postback string `json:"postback"`
}

type FulfillmentCard struct {
	Title    string              `json:"title"`
	Subtitle string              `json:"subtitle"`
	ImageURI string              `json:"imageUri"`
	Buttons  []FulfillmentButton `json:"buttons"`
}

type FulfillmentMessage struct {
	Card FulfillmentCard `json:"card"`
}

type AOGRequestAnswer struct {
	FulfillmentText     string               `json:"fulfillmentText,omitempty"`
	FulfillmentMessages []FulfillmentMessage `json:"fulfillmentMessages,omitempty"`
	Source              string               `json:"source,omitempty"`
	Payload             struct {
		Google struct {
			ExpectUserResponse bool `json:"expectUserResponse"`
			RichResponse       struct {
				Items       []RespItem   `json:"items,omitempty"`
				Suggestions []Suggestion `json:"suggestions,omitempty"`
			} `json:"richResponse"`
		} `json:"google"`
		// Facebook struct {
		// 	Text string `json:"text"`
		// } `json:"facebook"`
		// Slack struct {
		// 	Text string `json:"text"`
		// } `json:"slack"`
	} `json:"payload"`
	//OutputContexts []OutputContext `json:"outputContexts"`
	// FollowupEventInput struct {
	// 	Name         string            `json:"name"`
	// 	LanguageCode string            `json:"languageCode"`
	// 	Parameters   map[string]string `json:"parameters"`
	// } `json:"followupEventInput"`
}
