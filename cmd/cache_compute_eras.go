package cmd

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
)

var cacheComputeErasCmd = &cobra.Command{
	Use:   "compute-eras",
	Short: "Compute CLIP text embedding centroids for photo eras",
	Long: `Compute CLIP text embedding centroids for photo era estimation.

For each era (time period), generates 20 text prompts describing typical visual
characteristics of photos from that era, computes their CLIP text embeddings,
and averages them into a single centroid embedding per era. These centroids can
later be compared against photo image embeddings to estimate the era of a photo.

Examples:
  # Preview prompts and embeddings without saving
  photo-sorter cache compute-eras --dry-run

  # Compute and save era embeddings
  photo-sorter cache compute-eras

  # JSON output
  photo-sorter cache compute-eras --json`,
	RunE: runCacheComputeEras,
}

func init() {
	cacheCmd.AddCommand(cacheComputeErasCmd)

	cacheComputeErasCmd.Flags().Bool("json", false, "Output as JSON")
	cacheComputeErasCmd.Flags().Bool("dry-run", false, "Compute embeddings but don't save to database")
}

// eraDef defines an era with its label, representative date, and visual cues
type eraDef struct {
	Slug               string
	Name               string
	RepresentativeDate string // YYYY-MM-DD
	Cues               []string
}

var eras = []eraDef{
	{
		Slug: "1900s", Name: "1900s (1900-1909)", RepresentativeDate: "1904-06-15",
		Cues: []string{
			"sepia tones and formal poses",
			"glass plate negative artifacts",
			"Victorian-era clothing and settings",
			"long exposure motion blur",
			"hand-tinted color accents",
			"ornate studio backdrops",
			"stiff upright posture and serious expressions",
			"soft focus and vignetting",
		},
	},
	{
		Slug: "1910s", Name: "1910s (1910-1919)", RepresentativeDate: "1914-06-15",
		Cues: []string{
			"sepia or brown-toned prints",
			"World War I era uniforms and settings",
			"Edwardian fashion and hairstyles",
			"slightly less formal poses than 1900s",
			"early Kodak Brownie snapshot aesthetic",
			"postcard-format prints",
			"outdoor garden and street scenes",
			"hand-written captions on borders",
		},
	},
	{
		Slug: "1920s", Name: "1920s (1920-1929)", RepresentativeDate: "1924-06-15",
		Cues: []string{
			"silver gelatin print look",
			"flapper-era fashion and bobbed hair",
			"Art Deco architectural elements",
			"early automobile culture",
			"more relaxed candid snapshots",
			"white-bordered print format",
			"beach and leisure scenes",
			"sharp contrast black and white",
		},
	},
	{
		Slug: "1930s", Name: "1930s (1930-1939)", RepresentativeDate: "1934-06-15",
		Cues: []string{
			"Depression-era documentary style",
			"Dust Bowl and rural landscapes",
			"improved tonal range in prints",
			"early color Kodachrome experiments",
			"streamline moderne architecture",
			"fedora hats and tailored suits",
			"family gathered around radio",
			"deckle-edged print borders",
		},
	},
	{
		Slug: "1940s", Name: "1940s (1940-1949)", RepresentativeDate: "1944-06-15",
		Cues: []string{
			"World War II era military uniforms",
			"wartime rationing and home front scenes",
			"medium format camera quality",
			"pin-up poster aesthetic",
			"victory gardens and propaganda posters",
			"early wire photo grain",
			"women in factory work clothes",
			"small square snapshot format",
		},
	},
	{
		Slug: "1950s", Name: "1950s (1950-1959)", RepresentativeDate: "1954-06-15",
		Cues: []string{
			"early Kodachrome and Ektachrome color",
			"pastel suburban scenes",
			"finned automobiles and chrome details",
			"TV antennas on rooftops",
			"poodle skirts and crew cuts",
			"Brownie camera snapshot aesthetic",
			"slightly faded warm color palette",
			"small square or 3x5 print format",
		},
	},
	{
		Slug: "1960s", Name: "1960s (1960-1969)", RepresentativeDate: "1964-06-15",
		Cues: []string{
			"saturated Kodachrome colors",
			"mod fashion and psychedelic patterns",
			"space age and atomic design elements",
			"slightly oversaturated reds and blues",
			"Instamatic camera square format",
			"rounded white borders on prints",
			"outdoor barbecue and pool scenes",
			"early color TV in background",
		},
	},
	{
		Slug: "1970s", Name: "1970s (1970-1979)", RepresentativeDate: "1974-06-15",
		Cues: []string{
			"warm orange and brown color cast",
			"wood paneling and shag carpet interiors",
			"bell-bottom pants and wide collars",
			"faded color prints with yellow shift",
			"Polaroid instant photo aesthetic",
			"station wagons and muscle cars",
			"disco era fashion and settings",
			"slightly soft focus consumer lens quality",
		},
	},
	{
		Slug: "1980s", Name: "1980s (1980-1989)", RepresentativeDate: "1984-06-15",
		Cues: []string{
			"vivid oversaturated consumer film colors",
			"red-eye flash photography artifacts",
			"big hair and neon fashion",
			"early home computer and video game screens",
			"4x6 glossy print format",
			"date stamp in orange on photo corner",
			"mall and arcade background settings",
			"harsh direct flash with dark backgrounds",
		},
	},
	{
		Slug: "1990s", Name: "1990s (1990-1999)", RepresentativeDate: "1994-06-15",
		Cues: []string{
			"disposable camera grain and flash",
			"grunge and casual fashion",
			"early internet and desktop computers visible",
			"35mm point-and-shoot quality",
			"slightly green or cyan color cast",
			"matte or semi-gloss 4x6 prints",
			"CRT television screens in background",
			"flash photography at indoor events",
		},
	},
	{
		Slug: "2000-2004", Name: "2000-2004", RepresentativeDate: "2002-06-15",
		Cues: []string{
			"early digital camera low resolution",
			"JPEG compression artifacts",
			"slight color fringing and noise",
			"flip phone and early gadgets visible",
			"low dynamic range clipped highlights",
			"small image dimensions upscaled",
			"harsh built-in flash look",
			"early 2000s fashion and interiors",
		},
	},
	{
		Slug: "2005-2009", Name: "2005-2009", RepresentativeDate: "2007-06-15",
		Cues: []string{
			"improved digital camera quality",
			"MySpace era selfie aesthetic",
			"early smartphone photos",
			"HDR experiments and over-processing",
			"Facebook-era party and event photos",
			"point-and-shoot digital compact look",
			"slightly noisy indoor photos",
			"flat screen TV in background",
		},
	},
	{
		Slug: "2010-2014", Name: "2010-2014", RepresentativeDate: "2012-06-15",
		Cues: []string{
			"Instagram filter aesthetic",
			"smartphone camera quality leap",
			"faux-vintage and cross-process effects",
			"selfie culture and group selfies",
			"shallow depth of field portraits",
			"clean well-lit food photography",
			"tablets and modern smartphones visible",
			"high-resolution detailed images",
		},
	},
	{
		Slug: "2015-2019", Name: "2015-2019", RepresentativeDate: "2017-06-15",
		Cues: []string{
			"dual-camera smartphone portrait mode",
			"computational photography HDR",
			"clean modern minimalist aesthetic",
			"drone aerial photography",
			"ultra-wide angle smartphone lens",
			"night mode photography",
			"social media optimized composition",
			"4K resolution and high dynamic range",
		},
	},
	{
		Slug: "2020-2024", Name: "2020-2024", RepresentativeDate: "2022-06-15",
		Cues: []string{
			"face masks and social distancing",
			"video call screenshots and remote work",
			"advanced computational photography",
			"AI-enhanced smartphone photos",
			"periscope telephoto zoom",
			"cinematic mode video stills",
			"high megapixel sensor detail",
			"ProRAW and advanced mobile editing",
		},
	},
	{
		Slug: "2025-2029", Name: "2025-2029", RepresentativeDate: "2027-06-15",
		Cues: []string{
			"AI-generated and AI-edited elements",
			"ultra-high resolution sensors",
			"advanced generative fill and editing",
			"spatial computing and 3D capture",
			"next-generation HDR and color science",
			"AI-powered scene optimization",
			"wearable camera and always-on capture",
			"seamless multi-frame computational merge",
		},
	},
}

// promptTemplatesPlain are templates that don't use cues (only {label})
var promptTemplatesPlain = []string{
	"a photograph taken in the %s",
	"a %s film photograph",
	"a %s photo print scan",
	"a candid snapshot from the %s",
}

// promptTemplatesCue are templates that use both {label} and {cue}
var promptTemplatesCue = []string{
	"a photograph with %s photographic look, %s",
	"a documentary photograph from the %s, %s",
	"an amateur photograph from the %s, %s",
	"an old photograph from the %s, %s",
	"a vintage photograph from the %s, %s",
	"a photo from the %s showing typical %s",
}

const promptsPerEra = 20

// generatePrompts generates text prompts for an era
func generatePrompts(era eraDef) []string {
	var prompts []string

	// Add plain templates (no cue)
	for _, tmpl := range promptTemplatesPlain {
		prompts = append(prompts, fmt.Sprintf(tmpl, era.Name))
	}

	// Add cue-based templates
	for _, tmpl := range promptTemplatesCue {
		for _, cue := range era.Cues {
			prompts = append(prompts, fmt.Sprintf(tmpl, era.Name, cue))
			if len(prompts) >= promptsPerEra {
				return prompts
			}
		}
	}

	// Truncate to promptsPerEra
	if len(prompts) > promptsPerEra {
		prompts = prompts[:promptsPerEra]
	}

	return prompts
}

// computeCentroid averages a slice of embeddings and L2-normalizes the result
func computeCentroid(embeddings [][]float32) []float32 {
	if len(embeddings) == 0 {
		return nil
	}

	dim := len(embeddings[0])
	centroid := make([]float32, dim)

	for _, emb := range embeddings {
		for i, v := range emb {
			centroid[i] += v
		}
	}

	n := float32(len(embeddings))
	for i := range centroid {
		centroid[i] /= n
	}

	// L2-normalize
	var norm float64
	for _, v := range centroid {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range centroid {
			centroid[i] = float32(float64(centroid[i]) / norm)
		}
	}

	return centroid
}

// ComputeErasResult represents the result of a compute-eras operation
type ComputeErasResult struct {
	Success       bool              `json:"success"`
	ErasComputed  int               `json:"eras_computed"`
	PromptsTotal  int               `json:"prompts_total"`
	EmbeddingDim  int               `json:"embedding_dim"`
	Model         string            `json:"model"`
	Pretrained    string            `json:"pretrained"`
	DryRun        bool              `json:"dry_run"`
	Eras          []EraResultEntry  `json:"eras"`
	DurationMs    int64             `json:"duration_ms"`
	DurationHuman string            `json:"duration_human,omitempty"`
}

// EraResultEntry represents a single era in the result
type EraResultEntry struct {
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	RepresentativeDate string `json:"representative_date"`
	PromptsUsed        int    `json:"prompts_used"`
}

func runCacheComputeEras(cmd *cobra.Command, args []string) error {
	jsonOutput := mustGetBool(cmd, "json")
	dryRun := mustGetBool(cmd, "dry-run")

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	// Initialize PostgreSQL (needed even for dry-run to verify config)
	if cfg.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}
	if !dryRun {
		if err := postgres.Initialize(&cfg.Database); err != nil {
			return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
		}

		pool := postgres.GetGlobalPool()
		eraRepo := postgres.NewEraEmbeddingRepository(pool)
		database.RegisterPostgresBackend(
			nil,
			nil,
			nil,
		)
		database.RegisterEraEmbeddingWriter(func() database.EraEmbeddingWriter { return eraRepo })
	}

	// Create embedding client
	embClient := fingerprint.NewEmbeddingClient(cfg.Embedding.URL, "")

	if !jsonOutput {
		fmt.Printf("Embedding service: %s\n", cfg.Embedding.URL)
		if dryRun {
			fmt.Println("DRY RUN - embeddings will be computed but not saved")
		}
		fmt.Printf("Processing %d eras with %d prompts each (%d total embeddings)\n\n",
			len(eras), promptsPerEra, len(eras)*promptsPerEra)
	}

	var result ComputeErasResult
	result.DryRun = dryRun

	var model, pretrained string
	var dim int

	for i, era := range eras {
		prompts := generatePrompts(era)

		if !jsonOutput {
			fmt.Printf("[%d/%d] %s (%d prompts)...", i+1, len(eras), era.Name, len(prompts))
		}

		// Compute embedding for each prompt
		var embeddings [][]float32
		for j, prompt := range prompts {
			// Use metadata variant on first call to capture model info
			if model == "" {
				embResult, err := embClient.ComputeTextEmbeddingWithMetadata(ctx, prompt)
				if err != nil {
					return fmt.Errorf("failed to compute text embedding for era %s prompt %d: %w", era.Slug, j, err)
				}
				model = embResult.Model
				pretrained = embResult.Pretrained
				dim = embResult.Dim
				embeddings = append(embeddings, embResult.Embedding)
			} else {
				emb, err := embClient.ComputeTextEmbedding(ctx, prompt)
				if err != nil {
					return fmt.Errorf("failed to compute text embedding for era %s prompt %d: %w", era.Slug, j, err)
				}
				embeddings = append(embeddings, emb)
			}
		}

		// Compute centroid
		centroid := computeCentroid(embeddings)

		if !jsonOutput {
			fmt.Printf(" done (dim=%d)\n", len(centroid))
		}

		// Save to database
		if !dryRun {
			eraWriter, err := database.GetEraEmbeddingWriter(ctx)
			if err != nil {
				return fmt.Errorf("failed to get era embedding writer: %w", err)
			}

			stored := database.StoredEraEmbedding{
				EraSlug:            era.Slug,
				EraName:            era.Name,
				RepresentativeDate: era.RepresentativeDate,
				PromptCount:        len(prompts),
				Embedding:          centroid,
				Model:              model,
				Pretrained:         pretrained,
				Dim:                dim,
			}

			if err := eraWriter.SaveEra(ctx, stored); err != nil {
				return fmt.Errorf("failed to save era embedding for %s: %w", era.Slug, err)
			}
		}

		result.Eras = append(result.Eras, EraResultEntry{
			Slug:               era.Slug,
			Name:               era.Name,
			RepresentativeDate: era.RepresentativeDate,
			PromptsUsed:        len(prompts),
		})
	}

	duration := time.Since(startTime)
	result.Success = true
	result.ErasComputed = len(eras)
	result.PromptsTotal = len(eras) * promptsPerEra
	result.EmbeddingDim = dim
	result.Model = model
	result.Pretrained = pretrained
	result.DurationMs = duration.Milliseconds()
	result.DurationHuman = formatDuration(duration)

	if jsonOutput {
		result.DurationHuman = ""
		return outputJSON(result)
	}

	// Human-readable summary
	fmt.Println("\nCompute complete!")
	fmt.Printf("  Eras computed:   %d\n", result.ErasComputed)
	fmt.Printf("  Total prompts:   %d\n", result.PromptsTotal)
	fmt.Printf("  Embedding dim:   %d\n", result.EmbeddingDim)
	fmt.Printf("  Model:           %s\n", result.Model)
	fmt.Printf("  Pretrained:      %s\n", result.Pretrained)
	if dryRun {
		fmt.Printf("  Mode:            DRY RUN\n")
	}
	fmt.Printf("  Duration:        %s\n", result.DurationHuman)

	return nil
}
