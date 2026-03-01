package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/spf13/cobra"
)

var cacheComputeErasCmd = &cobra.Command{
	Use:   "compute-eras",
	Short: "Compute CLIP text embedding centroids for photo eras",
	Long: `Compute CLIP text embedding centroids for photo era estimation.

For each era (time period), generates 30 text prompts describing typical visual
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

// eraDef defines an era with its label, representative date, and visual cues.
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
			"sepia tones and formal rigid poses",
			"glass plate negative scratches and dust",
			"Victorian high-collar dresses and top hats",
			"long exposure ghostly motion blur",
			"hand-tinted pink cheeks on monochrome print",
			"ornate painted studio backdrop with columns",
			"cabinet card on thick cardboard mount",
			"gas lamp or candlelit interior lighting",
			"men in bowler hats and handlebar mustaches",
			"women in floor-length skirts and corsets",
			"heavy vignetting and soft corners",
			"daguerreotype-style metallic sheen on faces",
		},
	},
	{
		Slug: "1910s", Name: "1910s (1910-1919)", RepresentativeDate: "1914-06-15",
		Cues: []string{
			"sepia or warm brown-toned prints",
			"World War I military uniforms and helmets",
			"Edwardian high-waisted dresses and wide hats",
			"horse-drawn carriages alongside early automobiles",
			"early Kodak Brownie round-cornered snapshots",
			"postcard-format prints with divided backs",
			"outdoor garden party and croquet scenes",
			"hand-written ink captions on white borders",
			"soldiers wearing puttees and peaked caps",
			"low-contrast flat gray tonal range",
			"women with Gibson Girl upswept hairstyles",
			"small contact prints with rough edges",
		},
	},
	{
		Slug: "1920s", Name: "1920s (1920-1929)", RepresentativeDate: "1924-06-15",
		Cues: []string{
			"silver gelatin print with crisp tones",
			"flapper dresses and bobbed hair",
			"Art Deco geometric building facades",
			"Model T Ford and early touring cars",
			"white-bordered snapshot print format",
			"beach bathing costumes and boardwalks",
			"sharp high-contrast black and white",
			"cloche hats and fur-trimmed coats",
			"jazz nightclub and speakeasy interiors",
			"wide peaked lapel suits with pocket squares",
			"outdoor picnic and roadside scenes",
			"slightly soft lens and uneven exposure",
		},
	},
	{
		Slug: "1930s", Name: "1930s (1930-1939)", RepresentativeDate: "1934-06-15",
		Cues: []string{
			"Depression-era documentary style portraits",
			"Dust Bowl dry farmland landscapes",
			"rich tonal range with deep blacks and whites",
			"early Kodachrome warm saturated color slides",
			"streamline moderne rounded architecture",
			"fedora hats and double-breasted suits",
			"deckle-edged scalloped print borders",
			"large format sharp detailed negatives",
			"WPA murals and public building interiors",
			"bias-cut silk dresses and finger waves",
			"roadside diners and gas station signage",
			"sepia-toned group portraits on porches",
		},
	},
	{
		Slug: "1940s", Name: "1940s (1940-1949)", RepresentativeDate: "1944-06-15",
		Cues: []string{
			"World War II military uniforms and dog tags",
			"wartime victory garden and ration poster scenes",
			"medium format square sharp negatives",
			"pin-up poster painted illustration style",
			"women in factory coveralls and headscarves",
			"small square white-bordered snapshots",
			"rounded-fender sedans and military jeeps",
			"wide-shouldered padded suits and ties",
			"hand-colored tinted portrait prints",
			"grainy wire-service press photo look",
			"USO dance hall and canteen gatherings",
			"slightly yellowed matte print paper",
		},
	},
	{
		Slug: "1950s", Name: "1950s (1950-1959)", RepresentativeDate: "1954-06-15",
		Cues: []string{
			"early Kodachrome saturated red and blue slides",
			"pastel-painted suburban ranch houses",
			"chrome-finned Cadillac and Chevrolet cars",
			"TV antennas on rooftops",
			"poodle skirts and flat crew cuts",
			"Brownie camera slightly blurry snapshots",
			"faded warm yellowish color palette",
			"small square or 3x5 print format",
			"diner jukeboxes and chrome counter stools",
			"narrow knit ties and cardigan sweaters",
			"pastel Formica kitchen countertops",
			"drive-in movie theater screen at dusk",
		},
	},
	{
		Slug: "1960s", Name: "1960s (1960-1969)", RepresentativeDate: "1964-06-15",
		Cues: []string{
			"saturated Kodachrome vivid greens and reds",
			"mod miniskirts and go-go boots",
			"space age and atomic starburst decor",
			"oversaturated reds and blues with dense shadows",
			"Instamatic camera square format prints",
			"rounded white borders on color prints",
			"VW Beetle and wood-paneled station wagons",
			"tie-dye shirts and peace sign jewelry",
			"outdoor barbecue and backyard pool scenes",
			"magenta and cyan color shift on aged prints",
			"bouffant hairstyles and cat-eye sunglasses",
			"Ektachrome slide with bluish cast",
		},
	},
	{
		Slug: "1970s", Name: "1970s (1970-1979)", RepresentativeDate: "1974-06-15",
		Cues: []string{
			"warm orange and brown color cast",
			"wood paneling and shag carpet interiors",
			"bell-bottom pants and wide collar shirts",
			"faded color prints with yellow shift",
			"Polaroid instant photo white border",
			"station wagons and boxy muscle cars",
			"disco sequin outfits and platform shoes",
			"soft focus from cheap consumer lenses",
			"avocado green and harvest gold kitchen decor",
			"thick sideburns and feathered hair",
			"macrame wall hangings and houseplants",
			"tungsten orange cast under indoor lighting",
		},
	},
	{
		Slug: "1980s", Name: "1980s (1980-1989)", RepresentativeDate: "1984-06-15",
		Cues: []string{
			"vivid oversaturated consumer film colors",
			"red-eye flash photography artifacts",
			"big permed hair and neon clothing",
			"boxy car dashboards with velour seats",
			"4x6 glossy print format",
			"date stamp in orange on photo corner",
			"mall food court and arcade backgrounds",
			"harsh direct flash with dark backgrounds",
			"pastel Miami Vice style blazers",
			"aerobics leotards and leg warmers",
			"portable cassette player with headphones",
			"wood-grain TV console in living room",
		},
	},
	{
		Slug: "1990s", Name: "1990s (1990-1999)", RepresentativeDate: "1994-06-15",
		Cues: []string{
			"disposable camera grain and flash glare",
			"grunge flannel shirts and ripped jeans",
			"35mm point-and-shoot compact camera look",
			"slightly green or cyan color cast",
			"matte or semi-gloss 4x6 prints",
			"CRT television screens in background",
			"flash photography at indoor parties",
			"frosted hair tips and choker necklaces",
			"boxy minivans and rounded sedans",
			"washed-out faded pastel color palette",
			"brick-sized cell phones and pagers",
			"baggy cargo pants and platform sneakers",
		},
	},
	{
		Slug: "2000-2004", Name: "2000-2004", RepresentativeDate: "2002-06-15",
		Cues: []string{
			"early digital camera low resolution noise",
			"JPEG compression blocky artifacts",
			"slight purple fringing on edges",
			"flip phone and silver gadgets visible",
			"clipped blown-out white highlights",
			"small 640x480 pixel dimensions",
			"harsh built-in flash washed-out faces",
			"low-rise jeans and velour tracksuits",
			"LCD flat panel replacing bulky CRT monitors",
			"oversaturated digital reds and greens",
			"warm orange white balance cast indoors",
			"chunky plastic point-and-shoot camera look",
		},
	},
	{
		Slug: "2005-plus", Name: "2005+", RepresentativeDate: "2015-06-15",
		Cues: []string{
			"improved digital camera sharpness and resolution",
			"smartphone camera sharp detailed photos",
			"computational photography HDR balanced highlights",
			"bathroom mirror selfie with flash glare",
			"high megapixel fine texture detail",
			"vertical 9:16 tall framing for stories",
			"dual-camera synthetic bokeh portrait blur",
			"night mode bright handheld low-light shots",
			"selfie stick and group selfie angles",
			"noisy high-ISO indoor color grain",
			"faded vintage filter with raised black levels",
			"ultra-wide 0.5x group selfie shot",
		},
	},
}

// promptTemplatesPlain are templates that don't use cues (only {label}).
var promptTemplatesPlain = []string{
	"a photograph taken in the %s",
	"a %s film photograph",
	"a %s photo print scan",
	"a candid snapshot from the %s",
	"a family photo from the %s",
	"a %s photograph found in a photo album",
}

// promptTemplatesCue are templates that use both {label} and {cue}.
var promptTemplatesCue = []string{
	"a photograph with %s photographic look, %s",
	"a documentary photograph from the %s, %s",
	"an amateur photograph from the %s, %s",
	"an old photograph from the %s, %s",
	"a vintage photograph from the %s, %s",
	"a photo from the %s showing typical %s",
	"a scanned photograph from the %s, %s",
	"a %s snapshot with %s",
}

const promptsPerEra = 30

// generatePrompts generates text prompts for an era.
func generatePrompts(era eraDef) []string {
	var prompts []string

	// Add plain templates (no cue) — 6 prompts.
	for _, tmpl := range promptTemplatesPlain {
		prompts = append(prompts, fmt.Sprintf(tmpl, era.Name))
	}

	// Add cue-based templates — 24 prompts (12 cues × 2 uses each, 8 templates × 3 uses each).
	// Interleave: cycle through cues and templates together for even distribution.
	cueCount := len(era.Cues)
	tmplCount := len(promptTemplatesCue)
	needed := promptsPerEra - len(prompts)
	for i := 0; i < needed && i < cueCount*tmplCount; i++ {
		cue := era.Cues[i%cueCount]
		tmpl := promptTemplatesCue[i%tmplCount]
		prompts = append(prompts, fmt.Sprintf(tmpl, era.Name, cue))
	}

	// Truncate to promptsPerEra.
	if len(prompts) > promptsPerEra {
		prompts = prompts[:promptsPerEra]
	}

	return prompts
}

// computeCentroid averages a slice of embeddings and L2-normalizes the result.
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

	// L2-normalize.
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

// ComputeErasResult represents the result of a compute-eras operation.
type ComputeErasResult struct {
	Success       bool             `json:"success"`
	ErasComputed  int              `json:"eras_computed"`
	PromptsTotal  int              `json:"prompts_total"`
	EmbeddingDim  int              `json:"embedding_dim"`
	Model         string           `json:"model"`
	Pretrained    string           `json:"pretrained"`
	DryRun        bool             `json:"dry_run"`
	Eras          []EraResultEntry `json:"eras"`
	DurationMs    int64            `json:"duration_ms"`
	DurationHuman string           `json:"duration_human,omitempty"`
}

// EraResultEntry represents a single era in the result.
type EraResultEntry struct {
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	RepresentativeDate string `json:"representative_date"`
	PromptsUsed        int    `json:"prompts_used"`
}

// eraEmbeddingMeta holds model info captured during the first embedding computation.
type eraEmbeddingMeta struct {
	Model      string
	Pretrained string
	Dim        int
}

// computeEraEmbeddings computes text embeddings for an era's prompts and returns the centroid.
// On the first call (when meta.Model is empty), it captures model metadata.
func computeEraEmbeddings(
	ctx context.Context, embClient *fingerprint.EmbeddingClient,
	era eraDef, prompts []string, meta *eraEmbeddingMeta,
) ([]float32, error) {
	var embeddings [][]float32
	for j, prompt := range prompts {
		if meta.Model == "" {
			embResult, err := embClient.ComputeTextEmbeddingWithMetadata(ctx, prompt)
			if err != nil {
				return nil, fmt.Errorf("failed to compute text embedding for era %s prompt %d: %w", era.Slug, j, err)
			}
			meta.Model = embResult.Model
			meta.Pretrained = embResult.Pretrained
			meta.Dim = embResult.Dim
			embeddings = append(embeddings, embResult.Embedding)
		} else {
			emb, err := embClient.ComputeTextEmbedding(ctx, prompt)
			if err != nil {
				return nil, fmt.Errorf("failed to compute text embedding for era %s prompt %d: %w", era.Slug, j, err)
			}
			embeddings = append(embeddings, emb)
		}
	}
	return computeCentroid(embeddings), nil
}

// saveEraCentroid saves a computed era centroid to the database.
func saveEraCentroid(
	ctx context.Context, era eraDef, centroid []float32, promptCount int, meta *eraEmbeddingMeta,
) error {
	eraWriter, err := database.GetEraEmbeddingWriter(ctx)
	if err != nil {
		return fmt.Errorf("failed to get era embedding writer: %w", err)
	}

	stored := database.StoredEraEmbedding{
		EraSlug:            era.Slug,
		EraName:            era.Name,
		RepresentativeDate: era.RepresentativeDate,
		PromptCount:        promptCount,
		Embedding:          centroid,
		Model:              meta.Model,
		Pretrained:         meta.Pretrained,
		Dim:                meta.Dim,
	}

	if err := eraWriter.SaveEra(ctx, stored); err != nil {
		return fmt.Errorf("failed to save era embedding for %s: %w", era.Slug, err)
	}
	return nil
}

// cleanupStaleEras deletes era embeddings that are no longer in the current eras list.
func cleanupStaleEras(ctx context.Context, jsonOutput bool) error {
	eraWriter, err := database.GetEraEmbeddingWriter(ctx)
	if err != nil {
		return fmt.Errorf("failed to get era embedding writer for cleanup: %w", err)
	}

	allStored, err := eraWriter.GetAllEras(ctx)
	if err != nil {
		return fmt.Errorf("failed to list stored eras for cleanup: %w", err)
	}

	currentSlugs := make(map[string]bool, len(eras))
	for _, era := range eras {
		currentSlugs[era.Slug] = true
	}

	for i := range allStored {
		if !currentSlugs[allStored[i].EraSlug] {
			if err := eraWriter.DeleteEra(ctx, allStored[i].EraSlug); err != nil {
				return fmt.Errorf("failed to delete stale era %s: %w", allStored[i].EraSlug, err)
			}
			if !jsonOutput {
				fmt.Printf("Deleted stale era: %s\n", allStored[i].EraSlug)
			}
		}
	}
	return nil
}

// printComputeErasResult prints the human-readable summary of a compute-eras result.
func printComputeErasResult(result ComputeErasResult, dryRun bool) {
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
}

// initComputeErasDB initializes the database if not in dry-run mode.
func initComputeErasDB(cfg *config.Config, dryRun bool) error {
	if cfg.Database.URL == "" {
		return errors.New("DATABASE_URL environment variable is required")
	}
	if !dryRun {
		if err := postgres.Initialize(&cfg.Database); err != nil {
			return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
		}
		pool := postgres.GetGlobalPool()
		eraRepo := postgres.NewEraEmbeddingRepository(pool)
		database.RegisterPostgresBackend(nil, nil, nil)
		database.RegisterEraEmbeddingWriter(func() database.EraEmbeddingWriter { return eraRepo })
	}
	return nil
}

// processAllEras computes embeddings for all eras and returns era result entries.
func processAllEras(
	ctx context.Context, embClient *fingerprint.EmbeddingClient,
	meta *eraEmbeddingMeta, dryRun bool, jsonOutput bool,
) ([]EraResultEntry, error) {
	var entries []EraResultEntry
	for i, era := range eras {
		prompts := generatePrompts(era)
		if !jsonOutput {
			fmt.Printf("[%d/%d] %s (%d prompts)...", i+1, len(eras), era.Name, len(prompts))
		}

		centroid, err := computeEraEmbeddings(ctx, embClient, era, prompts, meta)
		if err != nil {
			return nil, err
		}

		if !jsonOutput {
			fmt.Printf(" done (dim=%d)\n", len(centroid))
		}

		if !dryRun {
			if err := saveEraCentroid(ctx, era, centroid, len(prompts), meta); err != nil {
				return nil, err
			}
		}

		entries = append(entries, EraResultEntry{
			Slug: era.Slug, Name: era.Name,
			RepresentativeDate: era.RepresentativeDate, PromptsUsed: len(prompts),
		})
	}
	return entries, nil
}

func runCacheComputeEras(cmd *cobra.Command, args []string) error {
	jsonOutput := mustGetBool(cmd, "json")
	dryRun := mustGetBool(cmd, "dry-run")

	ctx := context.Background()
	cfg := config.Load()
	startTime := time.Now()

	if err := initComputeErasDB(cfg, dryRun); err != nil {
		return err
	}

	embClient, err := fingerprint.NewEmbeddingClient(cfg.Embedding.URL, "")
	if err != nil {
		return fmt.Errorf("invalid embedding config: %w", err)
	}
	if !jsonOutput {
		fmt.Printf("Embedding service: %s\n", cfg.Embedding.URL)
		if dryRun {
			fmt.Println("DRY RUN - embeddings will be computed but not saved")
		}
		fmt.Printf("Processing %d eras with %d prompts each (%d total embeddings)\n\n",
			len(eras), promptsPerEra, len(eras)*promptsPerEra)
	}

	meta := &eraEmbeddingMeta{}
	entries, err := processAllEras(ctx, embClient, meta, dryRun, jsonOutput)
	if err != nil {
		return err
	}

	if !dryRun {
		if err := cleanupStaleEras(ctx, jsonOutput); err != nil {
			return err
		}
	}

	duration := time.Since(startTime)
	result := ComputeErasResult{
		Success: true, DryRun: dryRun, Eras: entries,
		ErasComputed: len(eras), PromptsTotal: len(eras) * promptsPerEra,
		EmbeddingDim: meta.Dim, Model: meta.Model, Pretrained: meta.Pretrained,
		DurationMs: duration.Milliseconds(), DurationHuman: formatDuration(duration),
	}

	if jsonOutput {
		result.DurationHuman = ""
		return outputJSON(result)
	}
	printComputeErasResult(result, dryRun)
	return nil
}
