package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
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
	useRedirectServer = flag.Bool("use-redirect-server", false, "enable using a redirect server instead of copying a token into stdin")
	redirectUrl = flag.String("redirect-url", "http://localhost:2434", "the address from which you can reach the redirect server from your browser (when performing the login)")
	redirectPort = flag.String("redirect-port", "2434", "port on which the oauth receiver server starts up (if set)")
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
	if *discordWebhook == "" {
		val := os.Getenv("DISCORD_WEBHOOK")
		if val == "" {
			log.Fatalf("No webhook provided, shutting down!")
		}

		*discordWebhook = val
	}
	
	discord.WebhookURL = *discordWebhook

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
	

	cacheFile, err := tokenCacheFile()
	if err != nil {
		log.Fatalf("Unable to get path to cached credential file. %v", err)
	}
	tok, err := tokenFromFile(cacheFile)
	if err != nil {
		// Checking the Env to see if we should use the server and what values
		// Only is USE_REDIRECT_SERVER is set we will read the others (and override and command passed in values)
		// Because argument passing with docker sucks, and the redirect server is required for docker, we allow env to override args
		// However we only override for those actually set
		val := strings.ToLower(os.Getenv("USE_REDIRECT_SERVER"))
		if val == "1" || val == "true" {
			*useRedirectServer = true
			
			val, exists := os.LookupEnv("REDIRECT_URL")
			if exists {
				*redirectUrl = val
			}

			val, exists = os.LookupEnv("REDIRECT_PORT")
			if exists {
				*redirectPort = val
			}
		}
		

		if *useRedirectServer {
			config.RedirectURL = *redirectUrl
			authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
			fmt.Println("Trying to get token from web")
			tok, err = getTokenFromWeb(config, authURL)
		} else {
			config.RedirectURL = "urn:ietf:wg:oauth:2.0:oob"
			authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
			fmt.Println("Trying to get token from prompt")
			tok, err = getTokenFromPrompt(config, authURL)
		}

		if err == nil {
			saveToken(cacheFile, tok)
		} else {
			log.Fatalf("Unable to retrieve Token, aborting: %v", err.Error())
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
				handleError(discord.Post(discord.PostOptions{ Content: msg }),"Discord-API error:")
				log.Printf("Stream %v got scheduled\n", value.Id)

				time.Sleep(time.Second) // Prevent weird dublication due to low throughput on discord webhook
			}
			if postLive {
				handleError(discord.Post(discord.PostOptions{ Content: "Live now @here" }), "Discord-API error:")
				log.Printf("Stream %v is live!\n", value.Id)

				time.Sleep(time.Second)
			}
			if postComplete {
				handleError(discord.Post(discord.PostOptions{ Content: "Stream Is Over, VOD shall remain, as always" }), "Discord-API error:")
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

// getTokenFromWeb uses Config to request a Token.
// It returns the retrieved Token.
func getTokenFromWeb(config *oauth2.Config, authURL string) (*oauth2.Token, error) {
	codeCh, err := startWebServer()
	if err != nil {
		fmt.Printf("Unable to start a web server.%v\n", err.Error())
		return nil, err
	}

	fmt.Println("Go to the following link in your browser. After completing the programm will continue")
	fmt.Println(authURL)

	// Wait for the web server to get the code.
	code := <-codeCh
	return exchangeToken(config, code)
}

// startWebServer starts a web server that listens on http://localhost:8080.
// The webserver waits for an oauth code in the three-legged auth flow.
func startWebServer() (codeCh chan string, err error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%v",*redirectPort))
	if err != nil {
		return nil, err
	}
	codeCh = make(chan string)

	go http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.FormValue("code")
		codeCh <- code // send code to OAuth flow
		listener.Close()
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Received code: %v\r\nYou can now safely close this browser window.", code)
	}))

	return codeCh, nil
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
