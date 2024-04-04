package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"time"

	"path/filepath"

	"github.com/ecnepsnai/discord"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

var (
	discordWebhook = flag.String("discord-webhook", "", "Discord Webhook (required)")
	persistFilePath = flag.String("persist-file-path", "persist/data.json", "allows the bot to ressume function after restart")
	secretFile = flag.String("secret-file", "client_secret.json", "google-api secretes file")
	tokenPath = flag.String("token-path", "persist/.credentials", "google-api token credentials folder")

)

type KnownStream struct {
	Id string `json:"id"`

	Status string `json:"status"`
	
	StartTime int64 `json:"startTime"`

}

type Persist struct {
	Streams map[string]KnownStream `json:"streams"`

	NextTime int64 `json:"nextTime"`
	NextId *string `json:"nextId,omitempty"`
}


	//   "lifeCycleStatusUnspecified" - No value or the value is unknown.
	//   "created" - Incomplete settings, but otherwise valid
	//   "ready" - Complete settings
	//   "testing" - Visible only to partner, may need special UI treatment
	//   "live" - Viper is recording; this means the "clock" is running
	//   "complete" - The broadcast is finished.
	//   "revoked" - This broadcast was removed by admin action
	//   "testStarting" - Transition into TESTING has been requested
	//   "liveStarting" - Transition into LIVE has been requested
const (
	StatusUnknown		string	= "lifeCycleStatusUnspecified"
	StatusCreated			= "created"
	StatusReady			= "ready"
	StatusTestingStarting		= "testStarting"
	StatusTesting			= "testing"
	StatusLiveStarting		= "liveStarting"
	StatusLive			= "live"
	StatusComplete			= "complete"
	StatusRevoked			= "revoked"
)

func main() {
	flag.Parse()
	log.Println("Starting up...")

	ctx := context.Background()

	b, err := os.ReadFile(*secretFile)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b, youtube.YoutubeReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	
	config.RedirectURL = "urn:ietf:wg:oauth:2.0:oob"

	cacheFile, err := tokenCacheFile()
	if err != nil {
		log.Fatalf("Unable to get path to cached credential file. %v", err)
	}
	tok, err := tokenFromFile(cacheFile)
	if err != nil {
		authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
			fmt.Println("Trying to get token from prompt")
			tok, err = getTokenFromPrompt(config, authURL)
		if err == nil {
			saveToken(cacheFile, tok)
		}
	}
	client := config.Client(ctx, tok)

	service, err := youtube.NewService(ctx, option.WithHTTPClient(client))

	if err != nil {
		log.Fatalf("Error creating new YouTube client: %v", err)
	}
	
	data,err := persistFromFile(*persistFilePath)
	if err != nil {
		data = &Persist { Streams: make(map[string]KnownStream) }
	}

	if *discordWebhook == "" {
		log.Fatalf("No webhook provided, shutting down!")
	}
	
	discord.WebhookURL = *discordWebhook
	
	if data.NextId == nil {
		data.NextTime = math.MaxInt64
	}

	log.Println("Setup complete, entering loop...")

	for {
		execute(data, service)

		// log.Println("Debug: execute run")
		savePersist(*persistFilePath, *data)

		comparison := time.Now().Add(time.Minute * 5).UTC().Unix()

		if comparison > data.NextTime {
			// A Stream is soon starting/live, so we will only sleep a short duration
			time.Sleep(time.Second * 15)
		} else {
			// No new stream soon, we will sleep longer
			time.Sleep(time.Minute * 5)
		}
	}
}

func execute(data *Persist, service *youtube.Service) {
	call := service.LiveBroadcasts.List([]string{"id", "snippet", "contentDetails", "status"}).
		Mine(true).
		MaxResults(5)
	
	response, err := call.Do()
	if err != nil {
		log.Printf("Error in calling the YT-API: %v\n", err.Error())
		return;
	}

	for i := len(response.Items) - 1; i >= 0; i-- {
		item := response.Items[i]
		start, err := time.Parse(time.RFC3339, item.Snippet.ScheduledStartTime)
		startStamp := int64(0)
		if err == nil {
			startStamp = start.UTC().Unix()
		}

		value, existed := data.Streams[item.Id]
		if !existed {
			value = KnownStream{ Status: StatusUnknown, StartTime: startStamp, Id: item.Id }	
		}

		// Update if the status has changed
		if startStamp != value.StartTime {
			// Updating the starttime, maybe if I want to update the timestamp in the OG message
			value.StartTime = startStamp
		}
		if newStatus := item.Status.LifeCycleStatus; value.Status != newStatus {
			// Update the state and do messages accordingly
			postMain, postLive, postComplete := false, false, false

			switch newStatus {
			case StatusUnknown: break
			case StatusCreated: break
			case StatusRevoked: break
			case StatusReady:
				postMain = true
				break
			case StatusTestingStarting: fallthrough
			case StatusTesting:
				if value.Status != StatusReady && value.Status != StatusTestingStarting {
					postMain = true
				}
				break
			case StatusLiveStarting: fallthrough
			case StatusLive:
				if value.Status == StatusLiveStarting {
					break
				}
				if value.Status != StatusReady && value.Status != StatusTestingStarting && value.Status != StatusTesting {
					postMain = true
				}
				postLive = true
				break
			case StatusComplete:
				if value.Status != StatusLive && value.Status != StatusLiveStarting {
					// We could do the live post, but this is pointless, as the stream is already offline
					if value.Status != StatusReady && value.Status != StatusTestingStarting && value.Status != StatusTesting {
						postMain = true
					}
				}

				postComplete = true
				break
			}


			if postMain {
				msg := fmt.Sprintf("Lukas going live at <t:%v:f> (in <t:%v:R>)\nhttps://youtu.be/%v", value.StartTime, value.StartTime, value.Id)
				discord.Post(discord.PostOptions{ Content: msg })
				log.Printf("Stream %v got scheduled\n", value.Id)

				time.Sleep(time.Second) // Prevent weird dublication due to low throughput on discord webhook
			}
			if postLive {
				discord.Post(discord.PostOptions{ Content: "Live now @here" })
				log.Printf("Stream %v is live!\n", value.Id)

				time.Sleep(time.Second)
			}
			if postComplete {
				discord.Post(discord.PostOptions{ Content: "Stream Is Over, VOD shall remain, as always" })
				log.Printf("Stream %v has ended\n", value.Id)

				time.Sleep(time.Second)
			}



			value.Status = item.Status.LifeCycleStatus
		}

		// Setting the next Stream timestamp
		switch value.Status {
		case StatusUnknown: fallthrough
		case StatusCreated: fallthrough
		case StatusRevoked: 
			break
		case StatusReady: fallthrough
		case StatusTestingStarting: fallthrough
		case StatusTesting: fallthrough
		case StatusLiveStarting: fallthrough
		case StatusLive:
			if data.NextTime >= value.StartTime {
				data.NextId = &value.Id
				data.NextTime = value.StartTime
			}
			// should not be nil, as it would otherwise set in the if above
			if *data.NextId == value.Id && data.NextTime != value.StartTime {
				data.NextTime = value.StartTime
			}
			break
		case StatusComplete:
			if data.NextId == nil {
				break
			}
			if *data.NextId == value.Id {
				// Clean up
				data.NextTime = math.MaxInt64
				data.NextId = nil
			}
			break
		}


		data.Streams[item.Id] = value
	}
}

// tokenFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func persistFromFile(file string) (*Persist, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &Persist{} 
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// saveToken uses a file path to create a file and store the
// token in it.
func savePersist(file string, persist Persist) {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(persist)
}

// Print the ID and title of each result in a list as well as a name that
// identifies the list. For example, print the word section name "Videos"
// above a list of video search results, followed by the video ID and title
// of each matching video.
func printIDs(sectionName string, matches map[string]KnownStream) {
	fmt.Printf("%v:\n", sectionName)
	for id, info := range matches {
		text := fmt.Sprintf("%v, %v: %v", info.StartTime, info.Status, info.Id)
		fmt.Printf("[%v] %v\n", id, text)
	}
	fmt.Printf("\n\n")
}


// Exchange the authorization code for an access token
func exchangeToken(config *oauth2.Config, code string) (*oauth2.Token, error) {
	tok, err := config.Exchange(context.Background(), code)
	if err != nil {
		log.Fatalf("Unable to retrieve token %v", err)
	}
	return tok, nil
}

// getTokenFromPrompt uses Config to request a Token and prompts the user
// to enter the token on the command line. It returns the retrieved Token.
func getTokenFromPrompt(config *oauth2.Config, authURL string) (*oauth2.Token, error) {
	var code string
	fmt.Printf("Go to the following link in your browser. After completing " +
		"the authorization flow, enter the authorization code on the command " +
		"line: \n%v\n", authURL)

	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}
	fmt.Println(authURL)
	return exchangeToken(config, code)
}

// tokenCacheFile generates credential file path/filename.
// It returns the generated credential path/filename.
func tokenCacheFile() (string, error) {
	tokenCacheDir := *tokenPath
	os.MkdirAll(tokenCacheDir, 0700)
	return filepath.Join(tokenCacheDir,
		url.QueryEscape("youtube-go.json")), nil
}

// tokenFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// saveToken uses a file path to create a file and store the
// token in it.
func saveToken(file string, token *oauth2.Token) {
	fmt.Println("trying to save token")
	fmt.Printf("Saving credential file to: %s\n", file)
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}


func handleError(err error, message string) {
  if message == "" {
    message = "Error making API call"
  }
  if err != nil {
    log.Fatalf(message + ": %v", err.Error())
  }
}
