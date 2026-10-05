CREATE VIEW warehouse_roles AS
SELECT
    w.name AS warehouse_name,
    -- A PostgreSQL enum array resolves to a named slice (UserRoleSlice) that
    -- carries its own Scan/Value. The view must keep the element type so the
    -- filter instantiates comparator.Slice[UserRole] rather than the
    -- non-comparable slice type, and so the scan leaves it unwrapped.
    w.allowed_roles
FROM warehouses w;
