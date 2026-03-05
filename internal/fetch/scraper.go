package fetch

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	"examtopics-downloader/internal/constants"
	"examtopics-downloader/internal/models"
	"examtopics-downloader/internal/utils"

	"github.com/PuerkitoBio/goquery"
	"github.com/cheggaaa/pb/v3"
)

func getDataFromLink(link string) *models.QuestionData {
	doc, err := ParseHTML(link, *client)
	if err != nil {
		log.Printf("Failed parsing HTML data from link: %v", err)
		return nil
	}

	var allQuestions []string
	doc.Find("li.multi-choice-item").Each(func(i int, s *goquery.Selection) {
		// Remove the "Most Voted" badge text before extracting choice text
		s.Find(".most-voted-answer-badge").Remove()
		allQuestions = append(allQuestions, utils.CleanText(s.Text()))
	})

	answerText := strings.TrimSpace(doc.Find(".correct-answer").Text())
	answer := ""
	if len(answerText) > 0 {
		answer = string(strings.ReplaceAll(strings.ReplaceAll(answerText, " ", ""), "\n", "")[0])
	}

	// Extract only "Question #: X\nTopic #: Y" from the header — skip the
	// boilerplate "Actual exam question from …" text and "[All X Questions]" link.
	header := strings.TrimSpace(doc.Find(".question-discussion-header > div").Text())
	header = strings.ReplaceAll(header, "\t", "")
	header = strings.ReplaceAll(header, "\u00a0", " ")

	// Parse each comment individually so they render as proper markdown bullets.
	comments := parseComments(doc)

	return &models.QuestionData{
		Title:        utils.CleanText(doc.Find("h1").Text()),
		Header:       header,
		Content:      utils.CleanText(doc.Find(".card-text").Text()),
		Questions:    allQuestions,
		Answer:       answer,
		Timestamp:    utils.CleanText(doc.Find(".discussion-meta-data > i").Text()),
		QuestionLink: link,
		Comments:     comments,
	}
}

// parseComments extracts each .comment-container and renders it as a markdown
// bullet so comments are never smashed into a single line.
func parseComments(doc *goquery.Document) string {
	var lines []string

	doc.Find(".comment-container").Each(func(i int, s *goquery.Selection) {
		username := strings.TrimSpace(s.Find(".comment-username").Text())
		if username == "" {
			return
		}

		// Badge: "Highly Voted" or "Most Recent"
		badge := strings.TrimSpace(s.Find(".badge-primary").Text())
		// Strip trailing icon text (FontAwesome renders as extra chars)
		if idx := strings.Index(badge, "  "); idx != -1 {
			badge = strings.TrimSpace(badge[:idx])
		}

		// Date — also replace non-breaking spaces
		date := strings.ReplaceAll(strings.TrimSpace(s.Find(".comment-date").Text()), "\u00a0", " ")

		// Selected answer letter
		selected := strings.TrimSpace(s.Find(".comment-selected-answers strong").Text())

		// Upvote count
		upvotes := strings.TrimSpace(s.Find(".upvote-count").Text())

		// Comment body — get inner HTML, replace <br> with newline, strip tags
		contentEl := s.Find(".comment-content")
		contentHTML, _ := contentEl.Html()
		contentHTML = strings.ReplaceAll(contentHTML, "<br/>", "\n")
		contentHTML = strings.ReplaceAll(contentHTML, "<br>", "\n")
		// Strip remaining HTML tags
		tagRe := strings.NewReplacer("<", "<", ">", ">") // keep angle brackets from entities
		_ = tagRe
		plainContent := goquery.NewDocumentFromNode(contentEl.Nodes[0]).Text()
		// Re-apply the br→newline on the raw HTML approach for accuracy
		plainContent = stripHTMLPreservingBR(contentHTML)
		plainContent = strings.ReplaceAll(plainContent, "\u00a0", " ")
		plainContent = strings.TrimSpace(plainContent)

		if plainContent == "" {
			return
		}

		// Build the bullet header
		parts := []string{fmt.Sprintf("**%s**", username)}
		if badge != "" {
			parts = append(parts, fmt.Sprintf("`%s`", badge))
		}
		if date != "" {
			parts = append(parts, fmt.Sprintf("— *%s*", date))
		}
		if selected != "" {
			parts = append(parts, fmt.Sprintf("— Selected: **%s**", selected))
		}
		if upvotes != "" && upvotes != "0" {
			parts = append(parts, fmt.Sprintf("(+%s)", upvotes))
		}

		lines = append(lines, "- "+strings.Join(parts, " "))

		// Blockquote each line of the comment body
		for _, bodyLine := range strings.Split(plainContent, "\n") {
			trimmed := strings.TrimSpace(bodyLine)
			if trimmed != "" {
				lines = append(lines, "  > "+trimmed)
			} else {
				lines = append(lines, "  >")
			}
		}
		lines = append(lines, "")
	})

	return strings.Join(lines, "\n")
}

// stripHTMLPreservingBR converts HTML to plain text, treating <br> as newlines.
func stripHTMLPreservingBR(rawHTML string) string {
	// Already replaced <br> with \n before calling this; now strip remaining tags
	re := strings.NewReplacer()
	_ = re
	// Simple tag stripper using a loop
	var result strings.Builder
	inTag := false
	for _, ch := range rawHTML {
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
		case !inTag:
			result.WriteRune(ch)
		}
	}
	// Collapse 3+ newlines to 2
	text := result.String()
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	// Decode HTML entities (e.g. &#39; → ' , &amp; → & , &#34; → ")
	return html.UnescapeString(text)
}

var counter int = 0 //start counter at 1
func getJSONFromLink(link string) []*models.QuestionData {
	initialResp := FetchURL(link, *client)

	var githubResp map[string]any
	err := json.Unmarshal(initialResp, &githubResp)
	if err != nil {
		log.Printf("error unmarshalling GitHub API response: %v", err)
		return nil
	}

	downloadURL, ok := githubResp["download_url"].(string)
	if !ok {
		log.Printf("couldn't find download_url in GitHub API response")
		return nil
	}

	jsonResp := FetchURL(downloadURL, *client)

	var content models.JSONResponse
	err = json.Unmarshal(jsonResp, &content)
	if err != nil {
		log.Printf("error unmarshalling the questions data: %v", err)
		return nil
	}

	fmt.Println("Processing content from:", downloadURL)

	var questions []*models.QuestionData

	if content.PageProps.Questions == nil {
		log.Printf("no questions found in JSON content")
		return nil
	}

	for _, q := range content.PageProps.Questions {
		var comments string
		for _, discussion := range q.Discussion {
			comments += fmt.Sprintf("[%s] %s\n", discussion.Poster, discussion.Content)
		}

		var choicesHeader string
		var keys []string
		for key := range q.Choices {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			choicesHeader += fmt.Sprintf("**%s:** %s\n\n", key, q.Choices[key])
		}

		name := utils.GetNameFromLink(link)
		counter++

		questions = append(questions, &models.QuestionData{
			Title:        "Examtopics " + strings.ReplaceAll(name, ".json?ref=main", "") + " question #" + strconv.Itoa(counter),
			Header:       q.QuestionText,
			Content:      strings.Join(q.QuestionImages, "\n"),
			Questions:    []string{choicesHeader},
			Answer:       q.Answer,
			Timestamp:    q.Timestamp,
			QuestionLink: q.URL,
			Comments:     utils.CleanText(comments),
		})
	}

	return questions
}

func fetchAllPageLinksConcurrently(providerName, grepStr string, numPages, concurrency int) []string {
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	results := make(chan []string, numPages)
	bar := pb.StartNew(numPages)
	startTime := utils.StartTime()

	rateLimiter := utils.CreateRateLimiter(constants.RequestsPerSecond)
	defer rateLimiter.Stop()

	for i := 1; i <= numPages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			<-rateLimiter.C

			url := fmt.Sprintf("https://www.examtopics.com/discussions/%s/%d", providerName, i)
			results <- getLinksFromPage(url, grepStr)
			bar.Increment()
		}(i)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// about 10 questions per examtopics page, we can preallocate
	all := make([]string, 0, numPages*10)
	for res := range results {
		all = append(all, res...)
	}

	bar.Finish()
	fmt.Printf("Scraping completed in %s.\n", utils.TimeSince(startTime))
	return all
}

// Main concurrent page scraping logic
func GetAllPages(providerName string, grepStr string) []models.QuestionData {
	baseURL := fmt.Sprintf("https://www.examtopics.com/discussions/%s/", providerName)
	numPages := getMaxNumPages(baseURL)
	fmt.Printf("Fetching %d pages for provider '%s'\n", numPages, providerName)

	allLinks := fetchAllPageLinksConcurrently(providerName, grepStr, numPages, constants.MaxConcurrentRequests)

	unique := utils.DeduplicateLinks(allLinks)
	sortedLinks := utils.SortLinksByQuestionNumber(unique)

	fmt.Printf("Found %d unique matching links:\n", len(sortedLinks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.MaxConcurrentRequests)
	results := make([]*models.QuestionData, len(sortedLinks))
	startTime := utils.StartTime()
	bar := pb.StartNew(len(sortedLinks))

	rateLimiter := utils.CreateRateLimiter(constants.RequestsPerSecond)
	defer rateLimiter.Stop()

	for i, link := range sortedLinks {
		wg.Add(1)
		url := utils.AddToBaseUrl(link)

		go func(i int, url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			<-rateLimiter.C

			data := getDataFromLink(url)
			if data != nil {
				results[i] = data
			}
			bar.Increment()
		}(i, url)
	}

	wg.Wait()
	bar.Finish()
	// Filter out nil entries
	var finalData []models.QuestionData
	for _, entry := range results {
		if entry != nil {
			finalData = append(finalData, *entry)
		}
	}

	fmt.Printf("Scraping completed in %s.\n", utils.TimeSince(startTime))

	return finalData
}
