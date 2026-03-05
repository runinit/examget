package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"examtopics-downloader/internal/fetch"
	"examtopics-downloader/internal/models"
	"examtopics-downloader/internal/utils"
)

// writeOutput handles the final write step for all output types.
// For obsidian / obsidian-sr it delegates to WriteObsidianVault;
// everything else goes through the existing WriteData function.
func writeOutput(links []models.QuestionData, outputPath, fileType, examName string, commentBool bool) {
	switch fileType {
	case "obsidian":
		utils.WriteObsidianVault(links, outputPath, examName, commentBool, false)
	case "obsidian-sr":
		utils.WriteObsidianVault(links, outputPath, examName, commentBool, true)
	default:
		utils.WriteData(links, outputPath, commentBool, fileType)
		fmt.Printf("Successfully saved output to %s (filetype: %s).\n", outputPath, fileType)
	}
}

func main() {
	provider := flag.String("p", "google", "Name of the exam provider (default -> google)")
	grepStr := flag.String("s", "", "String to grep for in discussion links")
	outputPath := flag.String("o", "examtopics_output.md", "Output path: file for md/pdf/html/txt, directory for obsidian/obsidian-sr")
	fileType := flag.String("type", "md", "Output format: md | pdf | html | text | obsidian | obsidian-sr")
	commentBool := flag.Bool("c", false, "Include all the comment/discussion text")
	examsFlag := flag.Bool("exams", false, "Show all possible exams for the selected provider and exit")
	saveUrls := flag.Bool("save-links", false, "Save unique question links to saved-links.txt")
	noCache := flag.Bool("no-cache", false, "Disable cached data lookup on GitHub")
	token := flag.String("t", "", "GitHub PAT for faster cached requests")
	linksFile := flag.String("links-file", "", "Skip link collection and scrape URLs listed in this file (one per line)")
	examName := flag.String("exam-name", "", "Exam name for Obsidian vault titles/tags (e.g. AZ-500). Defaults to -s value.")
	flag.Parse()

	// Default exam name to the search string if not provided
	resolvedExamName := *examName
	if resolvedExamName == "" {
		resolvedExamName = strings.ToUpper(*grepStr)
	}
	if resolvedExamName == "" {
		resolvedExamName = "Exam"
	}

	if *examsFlag {
		exams := fetch.GetProviderExams(*provider)
		fmt.Printf("Exams for provider '%s'\n\n", *provider)
		for _, exam := range exams {
			fmt.Println(utils.AddToBaseUrl(exam))
		}
		os.Exit(0)
	}

	// If a links file is provided, skip straight to content scraping
	if *linksFile != "" {
		f, err := os.Open(*linksFile)
		if err != nil {
			log.Fatalf("Failed to open links file %q: %v", *linksFile, err)
		}
		defer f.Close()

		var urls []string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				urls = append(urls, line)
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatalf("Error reading links file: %v", err)
		}

		fmt.Printf("Loaded %d URLs from %s\n", len(urls), *linksFile)
		links := fetch.GetPagesFromURLs(urls)
		writeOutput(links, *outputPath, *fileType, resolvedExamName, *commentBool)
		os.Exit(0)
	}

	if *grepStr == "" {
		log.Printf("running without a valid string to search for with -s, (no_grep_str)!")
	}

	if !*noCache {
		links := fetch.GetCachedPages(*provider, *grepStr, *token)
		if len(links) > 0 {
			writeOutput(links, *outputPath, *fileType, resolvedExamName, *commentBool)
			os.Exit(0)
		}
	}

	fmt.Println("Going to manual scraping, cached data failed.")
	links := fetch.GetAllPages(*provider, *grepStr)

	if *saveUrls {
		utils.SaveLinks("saved-links.txt", links)
	}
	writeOutput(links, *outputPath, *fileType, resolvedExamName, *commentBool)
}
