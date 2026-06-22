package copilot

import "testing"

func TestMatchMention(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		mentionTag string
		wantMatch  bool
		wantQuery  string
	}{
		{
			name:       "Start of sentence",
			content:    "@sai_coder help me write a function",
			mentionTag: "@sai_coder",
			wantMatch:  true,
			wantQuery:  "help me write a function",
		},
		{
			name:       "Middle of sentence",
			content:    "Hello @sai_coder, can you review this code?",
			mentionTag: "@sai_coder",
			wantMatch:  true,
			wantQuery:  "Hello , can you review this code?",
		},
		{
			name:       "End of sentence",
			content:    "Can you check this out @sai_coder",
			mentionTag: "@sai_coder",
			wantMatch:  true,
			wantQuery:  "Can you check this out",
		},
		{
			name:       "Substring conflict - should not match @sai",
			content:    "Ask @sai_coder for help",
			mentionTag: "@sai",
			wantMatch:  false,
			wantQuery:  "",
		},
		{
			name:       "Email conflict - should not match domain",
			content:    "contact me at myemail@domain.com",
			mentionTag: "@domain",
			wantMatch:  false,
			wantQuery:  "",
		},
		{
			name:       "With punctuation following mention",
			content:    "Hello @sai_coder!",
			mentionTag: "@sai_coder",
			wantMatch:  true,
			wantQuery:  "Hello !",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMatch, gotQuery := matchMention(tt.content, tt.mentionTag)
			if gotMatch != tt.wantMatch {
				t.Errorf("matchMention() gotMatch = %v, want %v", gotMatch, tt.wantMatch)
			}
			if gotQuery != tt.wantQuery {
				t.Errorf("matchMention() gotQuery = %q, want %q", gotQuery, tt.wantQuery)
			}
		})
	}
}
