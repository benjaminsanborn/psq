# [1.11.0](https://github.com/benjaminsanborn/psq/compare/v1.10.2...v1.11.0) (2026-11-06)


### Bug Fixes

* **statements:** hide psq's own queries from the Statements tab ([6d04d52](https://github.com/benjaminsanborn/psq/commit/6d04d52fca1e9528fb3ac5dd7f82f992613454fb))


### Features

* **active:** add lock tree, sorting and filtering to Active ([1c59355](https://github.com/benjaminsanborn/psq/commit/1c5935548c6cd856b2ffcc54905c02d78005e6fd))
* **cli:** run saved queries or SQL non-interactively ([b3656ba](https://github.com/benjaminsanborn/psq/commit/b3656ba1060a24262a7956094ab8829ddaa54174))
* fuzzy search, page keys and an embedded starter query pack ([0c11125](https://github.com/benjaminsanborn/psq/commit/0c11125a830856cdf058c8d3c9fa91b067c61e29))
* **refresh:** add pause, live interval control and per-query refresh ([271fa37](https://github.com/benjaminsanborn/psq/commit/271fa37add169f32a4766db18e53fadb34566f33))
* **safety:** run saved queries read-only with a statement timeout ([2c259d0](https://github.com/benjaminsanborn/psq/commit/2c259d06a04cfe744d5161c1066f7762ca0905ba))
* **statements:** add Statements tab with pg_stat_statements deltas ([7bc710b](https://github.com/benjaminsanborn/psq/commit/7bc710bb645eaebde84b5f7533efe78ad3c3fa7a))

## [1.10.2](https://github.com/benjaminsanborn/psq/compare/v1.10.1...v1.10.2) (2026-11-05)


### Bug Fixes

* **db:** honor .pgpass and sslmode from the service file ([f11d52c](https://github.com/benjaminsanborn/psq/commit/f11d52c503637e75bea41883b938792f42ac1c79))
* **query:** stop rename from duplicating queries and keep built-in tabs ([e2f2cd8](https://github.com/benjaminsanborn/psq/commit/e2f2cd8df88926edbbe86dcc691a25cf4d68b12f))
* **refresh:** keep model updates on the main goroutine and drop stale results ([f264830](https://github.com/benjaminsanborn/psq/commit/f2648305284e742ed6e89b753517341f93ae64e2))
* **ui:** measure display width when truncating and padding text ([577c231](https://github.com/benjaminsanborn/psq/commit/577c23155024f03d21e9c9a5499bebe10ba8996f))

## [1.10.1](https://github.com/benjaminsanborn/psq/compare/v1.10.0...v1.10.1) (2026-11-01)


### Bug Fixes

* **active:** apply process refresh on main goroutine to prevent index panic ([ecedc03](https://github.com/benjaminsanborn/psq/commit/ecedc0330258b778cf1a7d2174fbb0ec47592398))

# [1.10.0](https://github.com/benjaminsanborn/psq/compare/v1.9.3...v1.10.0) (2026-10-07)


### Features

* **db:** support AWS RDS IAM authentication ([282d9ae](https://github.com/benjaminsanborn/psq/commit/282d9aee71328ad13be305ba480db32e34203732))

## [1.9.3](https://github.com/benjaminsanborn/psq/compare/v1.9.2...v1.9.3) (2026-02-19)


### Bug Fixes

* address slow blocker query ([a59b2c7](https://github.com/benjaminsanborn/psq/commit/a59b2c7e2dce3fafb37834d2fe8f87625ee72a1e))

## [1.9.2](https://github.com/benjaminsanborn/psq/compare/v1.9.1...v1.9.2) (2026-02-19)


### Bug Fixes

* **ui:** prevent index out of range panic when selected exceeds queries length ([cab4785](https://github.com/benjaminsanborn/psq/commit/cab4785ae8aedcac5cfbd890ff11c35d2a76ed1f))

## [1.9.1](https://github.com/benjaminsanborn/psq/compare/v1.9.0...v1.9.1) (2026-02-19)


### Bug Fixes

* **active:** prevent nil pointer race in renderActiveView goroutine ([4423014](https://github.com/benjaminsanborn/psq/commit/4423014b140c03de02161be1184cb2fdca05bf84))

# [1.9.0](https://github.com/benjaminsanborn/psq/compare/v1.8.0...v1.9.0) (2026-02-17)


### Bug Fixes

* **db:** prevent nil pointer panic in runQuery goroutine ([c764c3b](https://github.com/benjaminsanborn/psq/commit/c764c3b5052a9718e7600ae03eacc19244c741b7))


### Features

* **active:** add interactive Active connections tab ([c0564aa](https://github.com/benjaminsanborn/psq/commit/c0564aa02cecbacfd833bd47d2b0ad56d6a01e84))

# [1.8.0](https://github.com/benjaminsanborn/psq/compare/v1.7.0...v1.8.0) (2026-02-15)


### Features

* show replication slot lag on primary instances ([17bf394](https://github.com/benjaminsanborn/psq/commit/17bf3940cba28e24a392383afbd7f0fef3fc5822))

# [1.7.0](https://github.com/benjaminsanborn/psq/compare/v1.6.0...v1.7.0) (2026-02-15)


### Features

* switch brew formula to dedicated homebrew-psq tap ([fb5794e](https://github.com/benjaminsanborn/psq/commit/fb5794e52d3e830ed94cb989f67d9068ec83b9a6))

# [1.6.0](https://github.com/benjaminsanborn/psq/compare/v1.5.0...v1.6.0) (2026-02-15)


### Features

* update brew tap, add --version flag, fix release workflow ([4879a7e](https://github.com/benjaminsanborn/psq/commit/4879a7e882c59350be1d2932cd0afebdeebcdda6))

# [1.5.0](https://github.com/benjaminsanborn/psq/compare/v1.4.0...v1.5.0) (2025-09-29)


### Features

* add gif and cleanup home ([30b89ce](https://github.com/benjaminsanborn/psq/commit/30b89ce490bd7e7ae9e269d3d7959aac01a62bde))

# [1.4.0](https://github.com/benjaminsanborn/psq/compare/v1.3.2...v1.4.0) (2025-09-10)


### Features

* use 5 mini now ([82b6cdd](https://github.com/benjaminsanborn/psq/commit/82b6cdd9c385712fbb1c8962ed6cb7b7825920dd))

## [1.3.2](https://github.com/benjaminsanborn/psq/compare/v1.3.1...v1.3.2) (2025-09-08)


### Bug Fixes

* remove cgo dependency ([a323645](https://github.com/benjaminsanborn/psq/commit/a3236455dcd0ac37a99d7b7ac3454c7a48321c1f))

## [1.3.1](https://github.com/benjaminsanborn/psq/compare/v1.3.0...v1.3.1) (2025-09-08)


### Bug Fixes

* C_GO flag ([085c12d](https://github.com/benjaminsanborn/psq/commit/085c12d07c0c6968974ce26cfdb91fd7ad4a6640))

# [1.3.0](https://github.com/benjaminsanborn/psq/compare/v1.2.0...v1.3.0) (2025-09-08)


### Features

* homebrew tap ([bea76bd](https://github.com/benjaminsanborn/psq/commit/bea76bd7b4c567ee3386f041f04b34c869457449))

# [1.2.0](https://github.com/benjaminsanborn/psq/compare/v1.1.1...v1.2.0) (2025-09-08)


### Features

* c to return to service picker ([e316651](https://github.com/benjaminsanborn/psq/commit/e316651a3f9f3c6a30cad095c79dd31c7853dfe7))
