package photoprism

import "fmt"

// GetSubject retrieves a single subject by UID
func (pp *PhotoPrism) GetSubject(uid string) (*Subject, error) {
	return doGetJSON[Subject](pp, "subjects/"+uid)
}

// UpdateSubject updates a subject's metadata
func (pp *PhotoPrism) UpdateSubject(uid string, update SubjectUpdate) (*Subject, error) {
	return doPutJSON[Subject](pp, "subjects/"+uid, update)
}

// GetSubjects retrieves subjects (people) from PhotoPrism
func (pp *PhotoPrism) GetSubjects(count int, offset int) ([]Subject, error) {
	endpoint := fmt.Sprintf("subjects?count=%d&offset=%d&type=person", count, offset)
	result, err := doGetJSON[[]Subject](pp, endpoint)
	if err != nil {
		return nil, err
	}
	return *result, nil
}
