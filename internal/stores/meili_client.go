package stores

import (
	"github.com/rs/zerolog/log"

	"github.com/meilisearch/meilisearch-go"
	"github.com/saybridge/saybridge/pkg/config"
)

// InitMeilisearch configures the Meilisearch client and sets up index attributes and settings.
func InitMeilisearch(cfg *config.Config) (meilisearch.ServiceManager, error) {
	client := meilisearch.New(cfg.MeiliURL, meilisearch.WithAPIKey(cfg.MeiliKey))

	indexUID := "messages"

	// Assert existence of search index
	_, err := client.GetIndex(indexUID)
	if err != nil {
		log.Info().Msgf("[Meilisearch] Index '%s' not found, creating index...", indexUID)
		_, err = client.CreateIndex(&meilisearch.IndexConfig{
			Uid:        indexUID,
			PrimaryKey: "message_id",
		})
		if err != nil {
			log.Warn().Err(err).Msg("[Meilisearch] Failed to create index")
		}
	}

	// Update index settings for full-text search, filtering, and sorting
	_, err = client.Index(indexUID).UpdateSettings(&meilisearch.Settings{
		SearchableAttributes: []string{"content", "sender_name"},
		FilterableAttributes: []string{"room_id", "tenant_id", "msg_type", "created_at"},
		SortableAttributes:   []string{"created_at"},
		TypoTolerance: &meilisearch.TypoTolerance{
			Enabled: true,
		},
		Pagination: &meilisearch.Pagination{
			MaxTotalHits: 5000,
		},
	})
	if err != nil {
		log.Warn().Err(err).Msg("[Meilisearch] Failed to update index settings")
	} else {
		log.Info().Msgf("[Meilisearch] Successfully initialized index '%s' and updated settings", indexUID)
	}

	return client, nil
}
