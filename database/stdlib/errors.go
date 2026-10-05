package stdlib

// MapError translates driver-specific errors from database/sql drivers into
// sqlgen error types. It checks for lib/pq, go-sql-driver/mysql, and
// modernc/sqlite error types in order.
// Returns (true, mapped error) when the error was recognized, or
// (false, original error) when it was not.
func MapError(err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	if ok, mapped := mapPqError(err); ok {
		return true, mapped
	}
	if ok, mapped := mapMySQLError(err); ok {
		return true, mapped
	}
	if ok, mapped := mapModerncSQLiteError(err); ok {
		return true, mapped
	}
	return false, err
}
