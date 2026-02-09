package ai

import "fmt"

// buildAlbumDatePrompt returns the embedded album date estimation prompt.
func buildAlbumDatePrompt() string {
	return albumDatePrompt
}

// buildAlbumDateContent builds the user message content for album date estimation.
// This is shared across all AI providers.
func buildAlbumDateContent(albumTitle, albumDescription string, photoDescriptions []string) string {
	content := fmt.Sprintf("Album title: %s\n", albumTitle)
	if albumDescription != "" {
		content += fmt.Sprintf("Album description: %s\n", albumDescription)
	}
	content += "\nPhoto descriptions:\n"
	for i, desc := range photoDescriptions {
		content += fmt.Sprintf("%d. %s\n", i+1, desc)
	}
	return content
}
