package photoprism

// Album represents a PhotoPrism album
type Album struct {
	UID         string `json:"UID"`
	Title       string `json:"Title"`
	Description string `json:"Description"`
	Favorite    bool   `json:"Favorite"`
	PhotoCount  int    `json:"PhotoCount"`
	Thumb       string `json:"Thumb"`
	Type        string `json:"Type"`
	CreatedAt   string `json:"CreatedAt"`
	UpdatedAt   string `json:"UpdatedAt"`
}

// Label represents a PhotoPrism label/tag
type Label struct {
	UID         string `json:"UID"`
	Name        string `json:"Name"`
	Slug        string `json:"Slug"`
	Description string `json:"Description"`
	Notes       string `json:"Notes"`
	PhotoCount  int    `json:"PhotoCount"`
	Favorite    bool   `json:"Favorite"`
	Priority    int    `json:"Priority"`
	CreatedAt   string `json:"CreatedAt"`
}

// Photo represents a PhotoPrism photo
type Photo struct {
	UID          string  `json:"UID"`
	Title        string  `json:"Title"`
	Description  string  `json:"Description"`
	TakenAt      string  `json:"TakenAt"`
	TakenAtLocal string  `json:"TakenAtLocal"`
	Favorite     bool    `json:"Favorite"`
	Private      bool    `json:"Private"`
	Type         string  `json:"Type"`
	Lat          float64 `json:"Lat"`
	Lng          float64 `json:"Lng"`
	Caption      string  `json:"Caption"`
	Year         int     `json:"Year"`
	Month        int     `json:"Month"`
	Day          int     `json:"Day"`
	Country      string  `json:"Country"`
	Hash         string  `json:"Hash"`
	Width        int     `json:"Width"`
	Height       int     `json:"Height"`
	OriginalName string  `json:"OriginalName"` // Original filename when uploaded
	FileName     string  `json:"FileName"`     // Current filename
	Name         string  `json:"Name"`         // Internal name
	Path         string  `json:"Path"`         // File path
	CameraModel  string  `json:"CameraModel"`  // Camera model name
	Scan         bool    `json:"Scan"`         // True if photo was scanned
}

// PhotoDetails represents additional photo details like notes
type PhotoDetails struct {
	Notes *string `json:"Notes,omitempty"`
}

// PhotoUpdate represents fields that can be updated on a photo
type PhotoUpdate struct {
	Title          *string       `json:"Title,omitempty"`
	Description    *string       `json:"Description,omitempty"`
	DescriptionSrc *string       `json:"DescriptionSrc,omitempty"`
	TakenAt        *string       `json:"TakenAt,omitempty"`
	TakenAtLocal   *string       `json:"TakenAtLocal,omitempty"`
	Favorite       *bool         `json:"Favorite,omitempty"`
	Private        *bool         `json:"Private,omitempty"`
	Lat            *float64      `json:"Lat,omitempty"`
	Lng            *float64      `json:"Lng,omitempty"`
	Caption        *string       `json:"Caption,omitempty"`
	CaptionSrc     *string       `json:"CaptionSrc,omitempty"`
	Year           *int          `json:"Year,omitempty"`
	Month          *int          `json:"Month,omitempty"`
	Day            *int          `json:"Day,omitempty"`
	Country        *string       `json:"Country,omitempty"`
	Altitude       *int          `json:"Altitude,omitempty"`
	TimeZone       *string       `json:"TimeZone,omitempty"`
	Details        *PhotoDetails `json:"Details,omitempty"`
}

// PhotoLabel represents a label/tag that can be added to a photo
type PhotoLabel struct {
	Name        string `json:"Name"`
	LabelSrc    string `json:"LabelSrc,omitempty"`
	Description string `json:"Description,omitempty"`
	Favorite    bool   `json:"Favorite,omitempty"`
	Notes       string `json:"Notes,omitempty"`
	Priority    int    `json:"Priority,omitempty"`
	Uncertainty int    `json:"Uncertainty,omitempty"`
}

// LabelUpdate represents fields that can be updated on a label
type LabelUpdate struct {
	Name        *string `json:"Name,omitempty"`
	Description *string `json:"Description,omitempty"`
	Notes       *string `json:"Notes,omitempty"`
	Priority    *int    `json:"Priority,omitempty"`
	Favorite    *bool   `json:"Favorite,omitempty"`
}

// Face represents a PhotoPrism face (face cluster with marker info)
type Face struct {
	ID              string  `json:"ID"`
	MarkerUID       string  `json:"MarkerUID"`
	FileUID         string  `json:"FileUID"`
	SubjUID         string  `json:"SubjUID"`
	Name            string  `json:"Name"`
	Src             string  `json:"Src"`
	SubjSrc         string  `json:"SubjSrc"`
	Hidden          bool    `json:"Hidden"`
	Size            int     `json:"Size"`
	Score           int     `json:"Score"`
	FaceDist        float64 `json:"FaceDist"`
	Samples         int     `json:"Samples"`
	SampleRadius    float64 `json:"SampleRadius"`
	Collisions      int     `json:"Collisions"`
	CollisionRadius float64 `json:"CollisionRadius"`
}

// Marker represents a face/subject region marker on a photo
type Marker struct {
	UID      string  `json:"UID"`
	FileUID  string  `json:"FileUID"`
	Type     string  `json:"Type"`
	Src      string  `json:"Src"`
	Name     string  `json:"Name"`
	SubjUID  string  `json:"SubjUID"`
	SubjSrc  string  `json:"SubjSrc"`
	FaceID   string  `json:"FaceID"`
	FaceDist float64 `json:"FaceDist"`
	X        float64 `json:"X"` // Relative X position (0-1)
	Y        float64 `json:"Y"` // Relative Y position (0-1)
	W        float64 `json:"W"` // Relative width (0-1)
	H        float64 `json:"H"` // Relative height (0-1)
	Size     int     `json:"Size"`
	Score    int     `json:"Score"`
	Invalid  bool    `json:"Invalid"`
	Review   bool    `json:"Review"`
}

// MarkerCreate represents the data needed to create a new marker
type MarkerCreate struct {
	FileUID string  `json:"FileUID"`
	Type    string  `json:"Type"`    // "face" for face markers
	X       float64 `json:"X"`       // Relative X position (0-1)
	Y       float64 `json:"Y"`       // Relative Y position (0-1)
	W       float64 `json:"W"`       // Relative width (0-1)
	H       float64 `json:"H"`       // Relative height (0-1)
	Name    string  `json:"Name"`    // Person name (optional)
	Src     string  `json:"Src"`     // Source: "manual", "image", etc.
	SubjSrc string  `json:"SubjSrc"` // Subject source: "manual" if user-assigned
}

// MarkerUpdate represents the data to update an existing marker
type MarkerUpdate struct {
	Name    string `json:"Name,omitempty"`    // Person name
	SubjSrc string `json:"SubjSrc,omitempty"` // Subject source: "manual" if user-assigned
	Invalid *bool  `json:"Invalid,omitempty"` // Set to true to mark as invalid/deleted
}

// Subject represents a PhotoPrism person/subject
type Subject struct {
	UID        string `json:"UID"`
	Name       string `json:"Name"`
	Slug       string `json:"Slug"`
	Thumb      string `json:"Thumb"`
	PhotoCount int    `json:"PhotoCount"`
	Favorite   bool   `json:"Favorite"`
	About      string `json:"About"`
	Alias      string `json:"Alias"`
	Bio        string `json:"Bio"`
	Notes      string `json:"Notes"`
	Hidden     bool   `json:"Hidden"`
	Private    bool   `json:"Private"`
	Excluded   bool   `json:"Excluded"`
	CreatedAt  string `json:"CreatedAt"`
	UpdatedAt  string `json:"UpdatedAt"`
}

// SubjectUpdate represents fields that can be updated on a subject
type SubjectUpdate struct {
	Name     *string `json:"Name,omitempty"`
	About    *string `json:"About,omitempty"`
	Alias    *string `json:"Alias,omitempty"`
	Bio      *string `json:"Bio,omitempty"`
	Notes    *string `json:"Notes,omitempty"`
	Favorite *bool   `json:"Favorite,omitempty"`
	Hidden   *bool   `json:"Hidden,omitempty"`
	Private  *bool   `json:"Private,omitempty"`
	Excluded *bool   `json:"Excluded,omitempty"`
}
