use google_youtube3 as youtube;
use youtube::{oauth2};
use hyper;
use hyper_rustls;
// test


#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let secret: oauth2::ApplicationSecret = Default::default();  

    let auth = oauth2::InstalledFlowAuthenticator::builder(
        secret,
        oauth2::InstalledFlowReturnMethod::Interactive
    ).build().await.unwrap();


    let mut hub = youtube::YouTube::new(hyper::Client::builder().build(hyper_rustls::HttpsConnectorBuilder::new().with_native_roots().https_or_http().enable_http1().build()), auth);
    

    Ok(())
}
