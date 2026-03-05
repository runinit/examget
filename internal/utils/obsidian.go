package utils

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"examtopics-downloader/internal/models"
)

// ─── helpers ────────────────────────────────────────────────────────────────

var topicRe = regexp.MustCompile(`topic-(\d+)`)
var questionRe = regexp.MustCompile(`question-(\d+)`)

// questionID returns a stable, sortable filename stem for a question.
// Uses topic+question from the URL when available, otherwise falls back to
// the sequential index (1-based, zero-padded to 4 digits).
func questionID(link string, idx int) string {
	tm := topicRe.FindStringSubmatch(link)
	qm := questionRe.FindStringSubmatch(link)
	if len(tm) > 1 && len(qm) > 1 {
		t, _ := strconv.Atoi(tm[1])
		q, _ := strconv.Atoi(qm[1])
		return fmt.Sprintf("T%03d-Q%04d", t, q)
	}
	return fmt.Sprintf("Q%04d", idx+1)
}

// safeWrite writes content to path, creating parent dirs as needed.
func safeWrite(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("mkdir %s: %v", filepath.Dir(path), err)
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		log.Printf("write %s: %v", path, err)
	}
}

// formatChoices renders the Questions slice as clean markdown bullet points.
// Each item in the slice is already a single choice line like "A. Option text".
func formatChoices(questions []string) string {
	var sb strings.Builder
	for _, q := range questions {
		trimmed := strings.TrimSpace(q)
		if trimmed != "" {
			sb.WriteString("- ")
			sb.WriteString(trimmed)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ─── Obsidian vault writer ───────────────────────────────────────────────────

// WriteObsidianVault exports the question list as a structured Obsidian vault:
//
//	<vaultDir>/
//	  questions/   T001-Q0010.md
//	  answers/     T001-Q0010-answer.md
//	  comments/    T001-Q0010-comments.md  (only when comments exist)
//	  flashcards.md                         (only when includeFlashcards=true)
//	  _index.md
//
// examName is used in tags and titles (e.g. "AZ-500").
func WriteObsidianVault(dataList []models.QuestionData, vaultDir, examName string, includeComments, includeFlashcards bool) {
	// Subdirectories
	questionsDir := filepath.Join(vaultDir, "questions")
	answersDir := filepath.Join(vaultDir, "answers")
	commentsDir := filepath.Join(vaultDir, "comments")

	for _, d := range []string{questionsDir, answersDir, commentsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatalf("failed to create vault directory %s: %v", d, err)
		}
	}

	// Tag slug: "AZ-500" → "az-500"
	tagSlug := strings.ToLower(strings.ReplaceAll(examName, " ", "-"))

	var flashcardBuf strings.Builder
	if includeFlashcards {
		flashcardBuf.WriteString(fmt.Sprintf("---\ntags:\n  - flashcards/%s\n---\n\n", tagSlug))
		flashcardBuf.WriteString(fmt.Sprintf("# %s Flashcards\n\n", examName))
		flashcardBuf.WriteString("> Practice deck for the Obsidian Spaced Repetition plugin.\n\n")
	}

	var indexRows []string

	for i, data := range dataList {
		if data.Title == "" {
			continue
		}

		id := questionID(data.QuestionLink, i)
		hasComments := includeComments && strings.TrimSpace(data.Comments) != ""

		// Wikilink stems (no .md extension — Obsidian resolves them)
		qLink := fmt.Sprintf("questions/%s", id)
		aLink := fmt.Sprintf("answers/%s-answer", id)
		cLink := fmt.Sprintf("comments/%s-comments", id)

		// ── Question note ──────────────────────────────────────────────────
		var q strings.Builder
		q.WriteString(fmt.Sprintf("---\ntags:\n  - exam/%s\n  - question\nexam: %s\nsource: %s\n---\n\n",
			tagSlug, examName, data.QuestionLink))

		// Title from header (e.g. "Question #: 10  Topic #: 1")
		q.WriteString(fmt.Sprintf("# %s — %s\n\n", examName, id))

		// Question body (the header field contains "Question #: X  Topic #: Y" etc.)
		if data.Header != "" {
			q.WriteString(data.Header)
			q.WriteString("\n\n")
		}

		// Question content text
		if data.Content != "" {
			q.WriteString(data.Content)
			q.WriteString("\n\n")
		}

		// Inline images
		for _, imgURL := range data.Images {
			q.WriteString(fmt.Sprintf("![](%s)\n\n", imgURL))
		}

		// Answer choices
		choices := formatChoices(data.Questions)
		if choices != "" {
			q.WriteString("## Options\n\n")
			q.WriteString(choices)
			q.WriteString("\n")
		}

		// References callout
		q.WriteString("> [!info] References\n")
		q.WriteString(fmt.Sprintf("> - 📝 [[%s|View Answer]]\n", aLink))
		if hasComments {
			q.WriteString(fmt.Sprintf("> - 💬 [[%s|View Discussion]]\n", cLink))
		}
		q.WriteString("\n")
		q.WriteString(fmt.Sprintf("*Posted: %s*\n", data.Timestamp))

		safeWrite(filepath.Join(questionsDir, id+".md"), q.String())

		// ── Answer note ────────────────────────────────────────────────────
		var a strings.Builder
		a.WriteString(fmt.Sprintf("---\ntags:\n  - exam/%s\n  - answer\nexam: %s\n---\n\n",
			tagSlug, examName))
		a.WriteString(fmt.Sprintf("# Answer — %s\n\n", id))
		a.WriteString(fmt.Sprintf("**Correct Answer: %s**\n\n", data.Answer))
		a.WriteString("---\n\n")
		a.WriteString(fmt.Sprintf("[[%s|← Back to Question]]", qLink))
		if hasComments {
			a.WriteString(fmt.Sprintf(" · [[%s|Discussion]]", cLink))
		}
		a.WriteString("\n")

		safeWrite(filepath.Join(answersDir, id+"-answer.md"), a.String())

		// ── Comments note ──────────────────────────────────────────────────
		if hasComments {
			var c strings.Builder
			c.WriteString(fmt.Sprintf("---\ntags:\n  - exam/%s\n  - discussion\nexam: %s\n---\n\n",
				tagSlug, examName))
			c.WriteString(fmt.Sprintf("# Discussion — %s\n\n", id))
			c.WriteString(fmt.Sprintf("[[%s|← Back to Question]] · [[%s|Answer]]\n\n", qLink, aLink))
			c.WriteString("---\n\n")
			c.WriteString(data.Comments)
			c.WriteString("\n")

			safeWrite(filepath.Join(commentsDir, id+"-comments.md"), c.String())
		}

		// ── Flashcard entry ────────────────────────────────────────────────
		if includeFlashcards {
			// Multi-line card: question (with options) / answer
			flashcardBuf.WriteString(fmt.Sprintf("<!-- %s -->\n", id))

			// Question side
			flashcardBuf.WriteString(fmt.Sprintf("**%s**: %s\n\n", id, data.Header))
			if data.Content != "" {
				flashcardBuf.WriteString(data.Content)
				flashcardBuf.WriteString("\n\n")
			}
			// Choices inline (A / B / C / D)
			for _, q2 := range data.Questions {
				t := strings.TrimSpace(q2)
				if t != "" {
					flashcardBuf.WriteString(t)
					flashcardBuf.WriteString("\n")
				}
			}
			flashcardBuf.WriteString("\n?\n\n")

			// Answer side
			flashcardBuf.WriteString(fmt.Sprintf("**Correct Answer: %s**\n\n", data.Answer))
			flashcardBuf.WriteString(fmt.Sprintf(
				"[[%s|View Question]] · [[%s|View Answer]]",
				qLink, aLink))
			if hasComments {
				flashcardBuf.WriteString(fmt.Sprintf(" · [[%s|Discussion]]", cLink))
			}
			flashcardBuf.WriteString("\n\n---\n\n")
		}

		// ── Index row ──────────────────────────────────────────────────────
		row := fmt.Sprintf("| [[%s\\|%s]] | [[%s\\|%s]] |", qLink, id, aLink, data.Answer)
		if hasComments {
			row += fmt.Sprintf(" [[%s\\|💬]] |", cLink)
		} else {
			row += " — |"
		}
		indexRows = append(indexRows, row)
	}

	// ── Write flashcards file ──────────────────────────────────────────────
	if includeFlashcards {
		safeWrite(filepath.Join(vaultDir, "flashcards.md"), flashcardBuf.String())
	}

	// ── Write index ────────────────────────────────────────────────────────
	var idx strings.Builder
	idx.WriteString(fmt.Sprintf("---\ntags:\n  - exam/%s\n  - index\n---\n\n", tagSlug))
	idx.WriteString(fmt.Sprintf("# %s — Question Index\n\n", examName))
	idx.WriteString(fmt.Sprintf("Total questions: **%d**\n\n", len(indexRows)))
	idx.WriteString("| Question | Answer | Discussion |\n")
	idx.WriteString("|----------|--------|------------|\n")
	for _, row := range indexRows {
		idx.WriteString(row)
		idx.WriteString("\n")
	}

	safeWrite(filepath.Join(vaultDir, "_index.md"), idx.String())

	fmt.Printf("Obsidian vault written to: %s\n", vaultDir)
	fmt.Printf("  questions/ : %d notes\n", len(indexRows))
	fmt.Printf("  answers/   : %d notes\n", len(indexRows))
	if includeFlashcards {
		fmt.Printf("  flashcards.md: %d cards\n", len(indexRows))
	}
}
