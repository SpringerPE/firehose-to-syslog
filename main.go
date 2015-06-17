package main

import (
	"fmt"
	"github.com/boltdb/bolt"
	"github.com/cloudfoundry-community/firehose-to-syslog/caching"
	"github.com/cloudfoundry-community/firehose-to-syslog/events"
	"github.com/cloudfoundry-community/firehose-to-syslog/firehose"
	"github.com/cloudfoundry-community/firehose-to-syslog/logging"
	"github.com/cloudfoundry-community/go-cfclient"
	"gopkg.in/alecthomas/kingpin.v2"
	"log"
	"os"
	"time"
)

var (
	debug             = kingpin.Flag("debug", "Enable debug mode. This disables forwarding to syslog").Default("false").Bool()
	apiEndpoint       = kingpin.Flag("api-address", "Api endpoint address.").Default("https://api.10.244.0.34.xip.io").String()
	dopplerAddress    = kingpin.Flag("doppler-address", "Overwrite default doppler endpoint return by /v2/info").String()
	syslogServer      = kingpin.Flag("syslog-server", "Syslog server.").String()
	subscriptionID    = kingpin.Flag("subscription-id", "ID for the subscription.").Default("firehose").String()
	user              = kingpin.Flag("user", "Admin user.").Default("admin").String()
	password          = kingpin.Flag("password", "Admin password.").Default("admin").String()
	skipSSLValidation = kingpin.Flag("skip-ssl-validation", "Please don't").Default("false").Bool()
	wantedEvents      = kingpin.Flag("events", fmt.Sprintf("Comma seperated list of events you would like. Valid options are %s", events.GetListAuthorizedEventEvents())).Default("LogMessage").String()
	boltDatabasePath  = kingpin.Flag("boltdb-path", "Bolt Database path ").Default("my.db").String()
	tickerTime        = kingpin.Flag("cc-pull-time", "CloudController Pooling time in sec").Default("60s").Duration()
)

const (
	version = "0.1.2-dev"
)

func main() {
	kingpin.Version(version)
	kingpin.Parse()
	logging.LogStd(fmt.Sprintf("Starting firehose-to-syslog %s ", version), true)

	logging.SetupLogging(*syslogServer, *debug)

	c := cfclient.Config{
		ApiAddress:        *apiEndpoint,
		Username:          *user,
		Password:          *password,
		SkipSslValidation: *skipSSLValidation,
	}
	cfClient := cfclient.NewClient(&c)

	dopplerEndpoint := cfClient.Endpoint.DopplerAddress
	if len(*dopplerAddress) > 0 {
		dopplerEndpoint = *dopplerAddress
	}
	logging.LogStd(fmt.Sprintf("Using %s as doppler endpoint", dopplerEndpoint), true)

	//Use bolt for in-memory  - file caching
	db, err := bolt.Open(*boltDatabasePath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		log.Fatal("Error opening bolt db: ", err)
		os.Exit(1)

	}
	defer db.Close()

	caching.Setup(cfClient, db)

	//Let's Update the database the first time
	logging.LogStd("Start filling app/space/org cache.", true)
	caching.Fill()
	logging.LogStd("Done filling cache!", true)

	logging.LogStd("Setting up event routing!", true)
	events.SetupEventRouting(*wantedEvents)

	// Ticker Pooling the CC every X sec
	ccPooling := time.NewTicker(*tickerTime)

	go func() {
		for range ccPooling.C {
			caching.Fill()
		}
	}()

	if logging.Connect() || *debug {

		logging.LogStd("Connected to Syslog Server! Connecting to Firehose...", true)

		firehose := firehose.CreateFirehoseChan(dopplerEndpoint, cfClient.GetToken(), *subscriptionID, *skipSSLValidation)
		if firehose != nil {
			logging.LogStd("Firehose Subscription Succesfull! Routing events...", true)
			events.LogEvents(firehose)
		} else {
			logging.LogError("Failed connecting to Firehose...Please check settings and try again!", "")
		}

	} else {
		logging.LogError("Failed connecting to the Syslog Server...Please check settings and try again!", "")
	}

}
