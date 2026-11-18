package server

// ModelList exposes the source project's model catalog to the afree-proxy
// gateway, which merges these realm-prefixed IDs into its public /v1/models.
func (h *Handler) ModelList() []map[string]any {
	if h == nil {
		return nil
	}
	return h.modelList()
}
