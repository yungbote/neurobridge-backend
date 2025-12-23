package index

import (
	"fmt"

	"github.com/google/uuid"
)

// ChatUserNamespace is the per-user namespace for chat retrieval docs.
// This prevents cross-tenant leakage and keeps Pinecone filters cheap.
func ChatUserNamespace(userID uuid.UUID) string {
	return fmt.Sprintf("chat:user:%s", userID.String())
}

