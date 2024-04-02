package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

var (
	// channelId = flag.String("channel-id", "UCQ00k_ZIuvbUyFgsfy_1DAQ", "Id of the channel we subscribe to")
)

func main() {
	flag.Parse()

	ctx := context.Background()

	b, err := os.ReadFile("client_secrets.json")
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

	// ForUsername and ForHandle are broken, and do not work
	// ManagedByMe will return an Auth error
	// Mine however for some god forsaken reason works
	// call := service.Channels.List([]string{"id", "snippet", "statistics", "contentDetails"}).
	// 	Mine(true)

	call := service.LiveBroadcasts.List([]string{"id", "snippet", "contentDetails", "status"}).
		Mine(true).
		MaxResults(25)
	
	response, err := call.Do()
	handleError(err, "")

	// Group video, channel, and playlist results in separate lists.
	videos := make(map[string]string)
	channels := make(map[string]string)
	playlists := make(map[string]string)



	fmt.Printf("%v: %v\n", response.ServerResponse.HTTPStatusCode, response.PageInfo.TotalResults)
	
	// if len(response.Items) > 0 {
	// 	channels[response.Items[0].Id] = response.Items[0].Snippet.Title
	// 	fmt.Printf("%v\n", response.Items[0].Statistics.SubscriberCount)
	// 	fmt.Printf("%v\n", response.Items[0].ContentDetails.RelatedPlaylists.WatchLater)
	// }

	// Iterate through each item and add it to the correct list.
	for _, item := range response.Items {
		text := fmt.Sprintf("%v, %v: %v", item.Snippet.ScheduledStartTime, item.Status.LifeCycleStatus, item.Snippet.Title)
		videos[item.Id] = text
	}

	printIDs("Videos", videos)
	printIDs("Channels", channels)
	printIDs("Playlists", playlists)
}

// Print the ID and title of each result in a list as well as a name that
// identifies the list. For example, print the word section name "Videos"
// above a list of video search results, followed by the video ID and title
// of each matching video.
func printIDs(sectionName string, matches map[string]string) {
	fmt.Printf("%v:\n", sectionName)
	for id, title := range matches {
		fmt.Printf("[%v] %v\n", id, title)
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
	tokenCacheDir := "persist/.credentials"
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
