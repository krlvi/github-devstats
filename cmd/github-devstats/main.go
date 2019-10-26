package main

import (
	"github.com/google/go-github/github"
	"github.com/krlvi/github-devstats/sql"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/krlvi/github-devstats/client"
	"github.com/krlvi/github-devstats/event"
)

func main() {
	if len(os.Args) < 3 {
		printUsageAndExit()
	}
	org := os.Args[1]
	accessToken := os.Args[2]
	if len(org) <= 0 || len(accessToken) <= 0 {
		printUsageAndExit()
	}
	daysAgo := 0
	if len(os.Args) > 3 {
		daysAgo, _ = strconv.Atoi(os.Args[3])
	}

	log.Println("fetching github teams and their members for", org)
	c := client.NewClient(org, accessToken)

	date := time.Now().AddDate(0, 0, daysAgo*-1)
	log.Println("fetching merged pull requests for", org, "on date", date.Format("2006-01-02"))
	prIssues, err := c.GetAllMergedPRIssues(date)
	if err != nil {
		log.Panicln("could not fetch pull requests:", err)
	}

	err = processIntoDB(c, prIssues)
	if err != nil {
		log.Println(err)
	}
}

func processIntoDB(c *client.GH, prIssues []github.Issue) error {
	log.Println("creating a db connection")
	db, err := sql.New()
	if err != nil {
		return err
	}
	events, err := sql.NewEventAccess(db)
	if err != nil {
		return err
	}
	for userId, teams := range c.GetTeamsByUser() {
		userName := c.GetUserName(userId)
		_ = events.SaveUser(userId, userName)
		for _, team := range teams {
			_ = events.SaveUserTeam(userId, team)
		}
	}
	ch := make(chan event.Event, 10)
	var wg sync.WaitGroup
	go sql.ReadAndPersist(events, ch, &wg)
	event.DumpEvents(c, prIssues, ch, &wg)
	wg.Wait()
	close(ch)
	return nil
}

func printUsageAndExit() {
	log.Println("supply github organization and access token as command parameters")
	os.Exit(1)
}
