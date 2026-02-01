package photoprism

import "fmt"

// GetFaces retrieves faces from PhotoPrism
func (pp *PhotoPrism) GetFaces(count int, offset int) ([]Face, error) {
	endpoint := fmt.Sprintf("faces?count=%d&offset=%d&hidden=yes&unknown=yes", count, offset)
	result, err := doGetJSON[[]Face](pp, endpoint)
	if err != nil {
		return nil, err
	}
	return *result, nil
}
