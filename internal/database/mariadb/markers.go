package mariadb

import (
	"context"
	"encoding/json"
	"fmt"
)

// UpdateMarkerEmbedding writes an InsightFace embedding to a PhotoPrism marker's embeddings_json field.
// The format is [[e1, e2, ..., e512]] (JSON list-of-lists, stored as mediumblob).
func (p *Pool) UpdateMarkerEmbedding(ctx context.Context, markerUID string, embedding []float32) error {
	// PhotoPrism stores embeddings as [[float, float, ...]] (list-of-lists)
	wrapped := [][]float32{embedding}
	data, err := json.Marshal(wrapped)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	// Verify marker exists first (MySQL RowsAffected returns 0 when data is unchanged)
	var exists bool
	err = p.db.QueryRowContext(ctx, `SELECT 1 FROM markers WHERE marker_uid = ? AND marker_type = 'face'`, markerUID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("marker %s not found", markerUID)
	}

	query := `UPDATE markers SET embeddings_json = ? WHERE marker_uid = ? AND marker_type = 'face'`
	if _, err := p.db.ExecContext(ctx, query, data, markerUID); err != nil {
		return fmt.Errorf("update marker embedding: %w", err)
	}

	return nil
}

// UpdateFaceClusterEmbedding writes a centroid embedding to a PhotoPrism face cluster's embedding_json field.
// The format is [e1, e2, ..., e512] (single JSON list).
func (p *Pool) UpdateFaceClusterEmbedding(ctx context.Context, faceID string, embedding []float32) error {
	data, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	// Verify face cluster exists first (MySQL RowsAffected returns 0 when data is unchanged)
	var exists bool
	err = p.db.QueryRowContext(ctx, `SELECT 1 FROM faces WHERE id = ?`, faceID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("face cluster %s not found", faceID)
	}

	query := `UPDATE faces SET embedding_json = ? WHERE id = ?`
	if _, err := p.db.ExecContext(ctx, query, data, faceID); err != nil {
		return fmt.Errorf("update face cluster embedding: %w", err)
	}

	return nil
}

// FaceCluster represents a PhotoPrism face cluster with its linked markers
type FaceCluster struct {
	ID         string
	MarkerUIDs []string
}

// GetFaceClusters returns all face clusters and their linked marker UIDs
func (p *Pool) GetFaceClusters(ctx context.Context) ([]FaceCluster, error) {
	query := `
		SELECT f.id, m.marker_uid
		FROM faces f
		JOIN markers m ON m.face_id = f.id
		WHERE m.marker_type = 'face'
		ORDER BY f.id, m.marker_uid
	`

	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query face clusters: %w", err)
	}
	defer rows.Close()

	clusterMap := make(map[string][]string)
	var order []string

	for rows.Next() {
		var faceID, markerUID string
		if err := rows.Scan(&faceID, &markerUID); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if _, exists := clusterMap[faceID]; !exists {
			order = append(order, faceID)
		}
		clusterMap[faceID] = append(clusterMap[faceID], markerUID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	clusters := make([]FaceCluster, 0, len(order))
	for _, id := range order {
		clusters = append(clusters, FaceCluster{
			ID:         id,
			MarkerUIDs: clusterMap[id],
		})
	}

	return clusters, nil
}
