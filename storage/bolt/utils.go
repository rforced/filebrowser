package bolt

import (
	bbolt "go.etcd.io/bbolt"
)

func nextUserID(tx *bbolt.Tx) (uint64, error) {
	b, err := tx.CreateBucketIfNotExists([]byte(usersBucket))
	if err != nil {
		return 0, err
	}
	meta, err := b.CreateBucketIfNotExists([]byte(metadataBucket))
	if err != nil {
		return 0, err
	}

	next := btoi(meta.Get([]byte(idCounterKey))) + 1
	if err := meta.Put([]byte(idCounterKey), itob(next)); err != nil {
		return 0, err
	}
	return next, nil
}

func bumpUserIDCounter(tx *bbolt.Tx, id uint64) error {
	b, err := tx.CreateBucketIfNotExists([]byte(usersBucket))
	if err != nil {
		return err
	}
	meta, err := b.CreateBucketIfNotExists([]byte(metadataBucket))
	if err != nil {
		return err
	}

	if btoi(meta.Get([]byte(idCounterKey))) >= id {
		return nil
	}
	return meta.Put([]byte(idCounterKey), itob(id))
}
