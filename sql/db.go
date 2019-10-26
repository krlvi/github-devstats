package sql

import (
	"database/sql"
	"github.com/krlvi/github-devstats/event"
	"github.com/krlvi/github-devstats/sql/commit"
	"github.com/krlvi/github-devstats/sql/pr"
	"github.com/krlvi/github-devstats/sql/schema"
	"log"
	"sync"
)

func New() (*sql.DB, error) {
	db, err := sql.Open("mysql", "devstats:devstats@tcp(127.0.0.1:3306)/devstats?multiStatements=true&parseTime=true")
	if err != nil {
		return nil, err
	}
	return db, nil
}

func ReadAndPersist(eventAccess *EventAccess, c chan event.Event, wg *sync.WaitGroup) {
	for {
		err := eventAccess.SavePREvent(<-c)
		if err != nil {
			log.Println(err)
		}
		wg.Done()
	}
}

type EventAccess struct {
	prs     *pr.Repo
	commits *commit.Repo
}

func NewEventAccess(db *sql.DB) (*EventAccess, error) {
	err := schema.MigrateUp(db)
	if err != nil && err.Error() != "no change" {
		return nil, err
	}
	return &EventAccess{
		prs:     pr.NewRepo(db),
		commits: commit.NewRepo(db),
	}, nil
}

func (a *EventAccess) SavePREvent(e event.Event) error {
	err := a.prs.Save(e)
	if err != nil {
		return err
	}
	log.Println("persisting merge event at", e.MergedAt, "repo", e.Repository, "pr", e.PrNumber)
	for commitType, count := range e.CommitsByType {
		err = a.commits.SavePrCommitByType(e.PrNumber, e.Repository, commitType, count)
		if err != nil {
			log.Println(err)
		}
	}
	for fileExt, count := range e.FilesAddedByExtension {
		err = a.commits.SavePrFilesAddedByExt(e.PrNumber, e.Repository, fileExt, count)
		if err != nil {
			log.Println(err)
		}
	}
	for fileExt, count := range e.FilesModifiedByExtension {
		err = a.commits.SavePrFilesModifiedByExt(e.PrNumber, e.Repository, fileExt, count)
		if err != nil {
			log.Println(err)
		}
	}
	return nil
}
