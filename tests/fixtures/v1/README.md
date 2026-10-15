# Version 1 compatibility fixtures

These fixed MongoDB Extended JSON documents model persisted version 1 data.
They are not generated from the current Go structs during tests. Explicit
`$date` and numeric wrappers preserve BSON types when loaded.

The integration contract re-applies current validators and indexes, reads the
existing surveys, publishes the historical draft, evaluates its routing rule,
submits all four answer types, and combines new answers with old answers in
statistics. It also verifies that the historical response remains unchanged.

`user.json` is an isolated test-only account: `legacy_owner` /
`legacy-password-123`. It is inserted only into the fixture's random test database.
Keep these fixtures stable as production models evolve; add a new fixture
version and explicit migration tests when a new persisted schema is introduced.
