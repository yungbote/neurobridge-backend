package chat_purge

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	vec pinecone.VectorStore
}

func New(db *gorm.DB, baseLog *logger.Logger, vec pinecone.VectorStore) *Pipeline {
	return &Pipeline{
		db:  db,
		log: baseLog.With("job", "chat_purge"),
		vec: vec,
	}
}

func (p *Pipeline) Type() string { return "chat_purge" }
