# TeamMofuMofu-Discord-Bot
A Discord Bot for Team Mofu Mofu... Well, currently it uses a Webhook to send messages.  

## Purpose/Features/Limitations
Currently only serves to post Live notifications from Youtube using Google API
- Only one Youtube Account supported, and this is the one that has to sign in
- And only one webhook
- If you set up the bot it will repost the 5 most recent streams
- But it will persist what it posted
- The persist/data.json is not automatically cleaned, so indefinitly grows (by only a handful of bytes per stream though)
- It's update interval is once every 5min, except when there is a stream scheduled strating <5min or live, then every 15s
- If you have multiple streams scheduled then the Going Live message is put underneath the most recently scheduled stream (which is not necessarily the live one)
- PRIVATE and UNLISTED streams will be announced (and linked) too!

## Running
A sample docker-compose.yaml is provided.  
You need to provide a `DISCORD_WEBHOOK` in the provided template. Be aware: Don't add paranthese, just put it in there raw.  
`REDIRECT_URL` and `REDIRECT_PORT` are optional variables (they have defaults), however important when deploying on a server, as redirect url has to be accessible from your browser in which you do auth.  
`USE_REDIRECT_SERVER` has to be set to 1 or true, else it would ask you to copy a token into stdin (which works fine when running locally, but not when within a container).  
These values (and additionally the `--token-path`, `--persist-file-path` and `--secret-file`) can be set as cli arguments (mainly for executing locally), with varying precedence over the Env values.  
  
Besides that you need to aquire a google API client_secret.json.  
Create a new project, add the You Tube Data API v3 to the available services.  
Configure a OAuth-Autherication screen, select the youtube-readonly api, add your google account to the test users. App can remain in testing.  
Create a new OAuth Client-ID, select Desktopapp (this is fine if you are doing localhost deployments)  
Then download the json and you just need a folder path for persist  
  
First setup will demand signing into yt account, it will print a link into the log output, open it in your browser and sign in.  
If everything goes right you get redirected at the end and the bot will spin up, if you end up on a 404 then the Redirect Url is incorrect.  
  
This bot will work with your App still in Testing, however the token will expire after 7 days,
requiring you to clear the persist/.credentials and log in again.  
Best publish the app on the OAuth-Autherication screen. It will tell you they will ask you for a bunch of information,
this is currently not needed, verification is optional.
  
### So, you are not doing localhost...
So you choose hell, so you need:
- Some sort of Domain (duckdns is sufficient)
- some way of generating tls certificates and applying them before rerouting to the container (caddy can do this, I deploy in k3s, so add an IngressRoute)
- the reroute can be to a local only dns subdomain (so you don't need to expose it to the internet, just remember to add a record to your local DNS)
- A new OAuth Client-ID that is a webapp, press edit on it, and then add the redirect url as a new Domain under Reroute URI
- Go back to the OAuth-Autherication screen config, edit it, and make sure your domain is included in the Authoriced domains (the value there is without any subdomain, this is correct) and press through to save
- Now you can download the json for the OAuth (if you downloaded it before adding a redirect you would need to edit the json to add any redirect to it, without redirect_uri within the json the program just fails)
- Don't forget to update `REDIRECT_URL`
- If it still returns 400: missmatch_redirect_uri then wait (because sometimes they take a long time), retrace your steps, curse at the sky and then just go to bed and hope it magically works tomorrow
