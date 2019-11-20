package event

import (
	"github.com/krlvi/github-devstats/sql/user"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/github"
	"github.com/krlvi/github-devstats/client"
)

type Event struct {
	PrNumber                 int            `json:"pr_number"`
	Repository               string         `json:"repository"`
	MergedAt                 time.Time      `json:"merged_at"`
	TimeToMergeSeconds       float64        `json:"time_to_merge_seconds"`
	BranchAgeSeconds         float64        `json:"branch_age_seconds"`
	LinesAdded               int            `json:"lines_added"`
	LinesRemoved             int            `json:"lines_removed"`
	FilesChanged             int            `json:"files_changed"`
	CommitsCount             int            `json:"commits_count"`
	CommentsCount            int            `json:"comments_count"`
	AuthorId                 string         `json:"author_id"`
	AuthorName               string         `json:"author_name"`
	AuthorTeams              []string       `json:"author_teams"`
	CommitsByType            map[string]int `json:"commits_by_type"`
	FilesAddedByExtension    map[string]int `json:"files_added_by_extension"`
	FilesModifiedByExtension map[string]int `json:"files_modified_by_extension"`
	JavaTestFilesModified    int            `json:"java_test_files_modified"`
	JavaTestsAdded           int            `json:"java_tests_added"`
	TimeToApproveSeconds     float64        `json:"time_to_approve_seconds"`
	ApproverId               string         `json:"approver_id"`
	ApproverName             string         `json:"approver_name"`
	ApproverTeams            []string       `json:"approver_teams"`
	CrossTeam                bool           `json:"cross_team"`
	DismissReviewCount       int            `json:"dismiss_review_count"`
	ChangesRequestedCount    int            `json:"changes_requested_count"`
}

func DumpEvents(c *client.GH, issues []github.Issue, ch chan Event, wg *sync.WaitGroup, users *user.Repo) {
	for _, i := range issues {
		repo := repoUrlToName(i.GetRepositoryURL())
		pr, err := c.GetPR(i.GetNumber(), repo)
		if err != nil {
			continue
		}
		wg.Add(1)
		ch <- prToEvent(c, pr, repo, users)
	}
}

func repoUrlToName(url string) string {
	tokens := strings.Split(url, "/")
	return tokens[len(tokens)-1]
}

func prToEvent(c *client.GH, p *github.PullRequest, repo string, users *user.Repo) Event {
	e := Event{
		PrNumber:                 p.GetNumber(),
		Repository:               repo,
		MergedAt:                 p.GetMergedAt().UTC(),
		TimeToMergeSeconds:       p.GetMergedAt().Sub(p.GetCreatedAt()).Seconds(),
		LinesAdded:               p.GetAdditions(),
		LinesRemoved:             p.GetDeletions(),
		FilesChanged:             p.GetChangedFiles(),
		CommitsCount:             p.GetCommits(),
		CommentsCount:            p.GetComments(),
		AuthorId:                 p.GetUser().GetLogin(),
		AuthorName:               users.GetName(p.GetUser().GetLogin()),
		AuthorTeams:              users.GetTeamsByUserId(p.GetUser().GetLogin()),
		CommitsByType:            map[string]int{},
		FilesAddedByExtension:    map[string]int{},
		FilesModifiedByExtension: map[string]int{},
		JavaTestFilesModified:    0,
		JavaTestsAdded:           0,
		TimeToApproveSeconds:     0,
		ApproverId:               "",
		ApproverName:             "",
		ApproverTeams:            nil,
		CrossTeam:                false,
		DismissReviewCount:       0,
		ChangesRequestedCount:    0,
	}

	commits, err := c.GetPRCommits(p.GetNumber(), repo)
	if err == nil {
		var firstCommit *github.Commit
		for _, com := range commits {
			e.CommitsByType[commitType(com.GetCommit().GetMessage())]++
			if firstCommit == nil {
				firstCommit = com.GetCommit()
			} else if com.GetCommit().GetCommitter().GetDate().Before(firstCommit.GetCommitter().GetDate()) {
				firstCommit = com.GetCommit()
			}
		}
		e.BranchAgeSeconds = branchAge(c, repo, firstCommit, p.GetMergeCommitSHA())
	}

	files, err := c.GetPRFiles(p.GetNumber(), repo)
	if err == nil {
		for _, f := range files {
			fileExt := fileExtension(f.GetFilename())
			if f.GetStatus() == "modified" {
				e.FilesModifiedByExtension[fileExt]++
				if strings.HasSuffix(f.GetFilename(), "Test.java") {
					e.JavaTestFilesModified++
				}
			}
			if f.GetStatus() == "added" {
				e.FilesAddedByExtension[fileExt]++
			}
			if strings.HasSuffix(f.GetFilename(), "Test.java") {
				e.JavaTestsAdded = javaTestsAddedInPatch(f.GetPatch())
			}
		}
	}

	reviews, err := c.GetReviews(p.GetNumber(), repo)
	if err == nil {
		for _, r := range reviews {
			if r.GetState() == "APPROVED" {
				e.TimeToApproveSeconds = r.GetSubmittedAt().Sub(p.GetCreatedAt()).Seconds()
				e.ApproverId = r.GetUser().GetLogin()
				e.ApproverName = users.GetName(r.GetUser().GetLogin())
				e.ApproverTeams = users.GetTeamsByUserId(r.GetUser().GetLogin())
				e.CrossTeam = crossTeam(users.GetTeamsByUserId(p.GetUser().GetLogin()), users.GetTeamsByUserId(r.GetUser().GetLogin()))
			}
			if r.GetState() == "DISMISSED" {
				e.DismissReviewCount++
			}
			if r.GetState() == "CHANGES_REQUESTED" {
				e.ChangesRequestedCount++
			}
		}
	}
	return e
}

func javaTestsAddedInPatch(patch string) int {
	testAdded := regexp.MustCompile(`^\+\s*@Test`)
	testRemoved := regexp.MustCompile(`^-\s*@Test`)
	return testsAddedInPatch(patch, testAdded, testRemoved)
}

func goTestsAddedInPatch(patch string) int {
	testAdded := regexp.MustCompile(`^\+\s*func\s*Test.*\*testing\.T\)\s*\{`)
	testRemoved := regexp.MustCompile(`^\-\s*func\s*Test.*\*testing\.T\)\s*\{`)
	return testsAddedInPatch(patch, testAdded, testRemoved)
}

func testsAddedInPatch(patch string, testAdded, testRemoved *regexp.Regexp) int {
	added := 0
	removed := 0
	for _, line := range strings.Split(patch, "\n") {
		if testAdded.MatchString(line) {
			added++
		} else if testRemoved.MatchString(line) {
			removed++
		}
	}
	return added - removed
}

func crossTeam(from, to []string) bool {
	fromSet := map[string]bool{}
	for _, f := range from {
		fromSet[f] = true
	}
	for _, t := range to {
		if fromSet[t] {
			return false
		}
	}
	return true
}

func fileExtension(filename string) string {
	tokens := strings.FieldsFunc(filename, delimiter)
	return tokens[len(tokens)-1]
}

func delimiter(r rune) bool {
	return r == '.' || r == '/'
}

func commitType(msg string) string {
	if strings.HasPrefix(msg, "build") {
		return "build"
	}
	if strings.HasPrefix(msg, "chore") {
		return "chore"
	}
	if strings.HasPrefix(msg, "ci") {
		return "ci"
	}
	if strings.HasPrefix(msg, "copy") {
		return "copy"
	}
	if strings.HasPrefix(msg, "doc") {
		return "docs"
	}
	if strings.HasPrefix(msg, "fea") {
		return "feat"
	}
	if strings.HasPrefix(msg, "fix") {
		return "fix"
	}
	if strings.HasPrefix(msg, "log") {
		return "log"
	}
	if strings.HasPrefix(msg, "perf") {
		return "perf"
	}
	if strings.HasPrefix(msg, "ref") {
		return "refactor"
	}
	if strings.HasPrefix(msg, "revert") {
		return "revert"
	}
	if strings.HasPrefix(msg, "style") {
		return "style"
	}
	if strings.HasPrefix(msg, "test") {
		return "test"
	}
	return "uncategorized"
}

func branchAge(c *client.GH, repo string, start *github.Commit, merge string) float64 {
	mergeCommit, err := c.GetCommit(repo, merge)
	if err != nil {
		return -1
	}
	return mergeCommit.GetCommitter().GetDate().Sub(start.GetCommitter().GetDate()).Seconds()
}
