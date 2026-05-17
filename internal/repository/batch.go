package repository

const defaultInsertBatchSize = 500

func insertBatchSize(model any) int {
	return defaultInsertBatchSize
}
