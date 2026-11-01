package world

import "errors"

// CheckBudget enforces exact file reservations and the fixed photo allowance.
func CheckBudget(total int64, p RoomPackage) error {
	if total < 0 || total > MaxSafeInteger || p.DescriptorBytes <= 0 || p.DescriptorBytes > MaxSafeInteger-PhotoReserve {
		return errors.New("invalid content byte count")
	}
	size := p.DescriptorBytes + PhotoReserve
	for _, a := range p.Assets {
		if a.Bytes <= 0 || a.Bytes > MaxSafeInteger-size {
			return errors.New("invalid asset byte count or overflow")
		}
		size += a.Bytes
	}
	if size > total/10 {
		return errors.New("package including photo reserve exceeds cache budget")
	}
	return nil
}
