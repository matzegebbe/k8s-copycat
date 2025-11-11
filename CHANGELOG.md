# Changelog

## [0.5.1](https://github.com/matzegebbe/k8s-copycat/compare/v0.5.0...v0.5.1) (2025-11-11)


### Documentation

* add cross-account irsa guide ([#118](https://github.com/matzegebbe/k8s-copycat/issues/118)) ([e5c856e](https://github.com/matzegebbe/k8s-copycat/commit/e5c856ed425a2b06749cfa371659cfc387618ed3))


### Miscellaneous

* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#122](https://github.com/matzegebbe/k8s-copycat/issues/122)) ([760ee63](https://github.com/matzegebbe/k8s-copycat/commit/760ee63a0482f097d59f8f53b220f122655acd62))

## [0.5.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.4.4...v0.5.0) (2025-11-05)


### Features

* avoid redundant pulls when mirroring platform subsets ([#116](https://github.com/matzegebbe/k8s-copycat/issues/116)) ([c8bad26](https://github.com/matzegebbe/k8s-copycat/commit/c8bad26942b5c16d53bbf7936043d7ff7b4456d9))


### Documentation

* add quickstart and observability guidance ([#115](https://github.com/matzegebbe/k8s-copycat/issues/115)) ([b1709c0](https://github.com/matzegebbe/k8s-copycat/commit/b1709c0980bf781a5912c53e2f7b290047a4df40))


### Miscellaneous

* **deps:** bump the go-patch group with 3 updates ([#113](https://github.com/matzegebbe/k8s-copycat/issues/113)) ([9547740](https://github.com/matzegebbe/k8s-copycat/commit/9547740bcebf0e1b5d454a4739061ff2b764b6d5))

## [0.4.4](https://github.com/matzegebbe/k8s-copycat/compare/v0.4.3...v0.4.4) (2025-11-04)


### Miscellaneous

* **deps:** bump helm/kind-action from 1.12.0 to 1.13.0 ([#108](https://github.com/matzegebbe/k8s-copycat/issues/108)) ([98f6ffb](https://github.com/matzegebbe/k8s-copycat/commit/98f6ffbf72a016b0871a37acc5b3f7cbe533f8e1))
* **deps:** bump sigs.k8s.io/controller-runtime in the go-patch group ([#109](https://github.com/matzegebbe/k8s-copycat/issues/109)) ([26cafe0](https://github.com/matzegebbe/k8s-copycat/commit/26cafe0410d1c348ee16071975adc14b180cf0c2))

## [0.4.3](https://github.com/matzegebbe/k8s-copycat/compare/v0.4.2...v0.4.3) (2025-11-03)


### Bug Fixes

* **manifest:** expand node permissions to include list and watch ([6de1d86](https://github.com/matzegebbe/k8s-copycat/commit/6de1d865a4717f6bbc21e3abc6c51ee26df12403))
* **manifest:** expand node permissions to include list and watch ([681e6c0](https://github.com/matzegebbe/k8s-copycat/commit/681e6c0f36b6e89a06f957fd0b205073b0f97a80))


### Miscellaneous

* **manifest:** add several options to manifest ([#106](https://github.com/matzegebbe/k8s-copycat/issues/106)) ([22f7871](https://github.com/matzegebbe/k8s-copycat/commit/22f78714ea4dc14596e9a8f1e379520684172039))

## [0.4.2](https://github.com/matzegebbe/k8s-copycat/compare/v0.4.1...v0.4.2) (2025-10-31)


### Miscellaneous

* **deps:** bump the go-patch group with 3 updates ([5518565](https://github.com/matzegebbe/k8s-copycat/commit/551856523f3b971be1241a410e20be1a982bb4f5))
* **deps:** bump the go-patch group with 3 updates ([8f25310](https://github.com/matzegebbe/k8s-copycat/commit/8f253107467e8db3ed73e333030b04d5e13214fe))

## [0.4.1](https://github.com/matzegebbe/k8s-copycat/compare/v0.4.0...v0.4.1) (2025-10-30)


### Bug Fixes

* **mirror:** scope mirrorPlatforms filtering to digest pulls ([067f7ac](https://github.com/matzegebbe/k8s-copycat/commit/067f7acf74391ae3aa228a4d27f4abc40435bfaa))
* **mirror:** scope mirrorPlatforms filtering to digest pulls ([894ae6a](https://github.com/matzegebbe/k8s-copycat/commit/894ae6aa998bc5b962423251268deca9f5409681))

## [0.4.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.3.3...v0.4.0) (2025-10-28)


### Features

* allow nodePlatform ([42c7ff0](https://github.com/matzegebbe/k8s-copycat/commit/42c7ff0fca76471fbfc797994d73e3e872434e06))


### Miscellaneous

* allow node reads for platform checks ([91721e2](https://github.com/matzegebbe/k8s-copycat/commit/91721e2e3412da4b220475c91472c7ff730103b3))
* allow node reads for platform checks ([eaa218b](https://github.com/matzegebbe/k8s-copycat/commit/eaa218b1c22655291e9bea2114a915b8f82fc48d))

## [0.3.3](https://github.com/matzegebbe/k8s-copycat/compare/v0.3.2...v0.3.3) (2025-10-26)


### Documentation

* add example ([1450733](https://github.com/matzegebbe/k8s-copycat/commit/14507333e42b1e7de8997a620c2c14020190ea04))
* add example ([09c685a](https://github.com/matzegebbe/k8s-copycat/commit/09c685a23ce177ca590dcb0df95b0dd2d4a0687d))
* clarify registry credential guidance ([505123e](https://github.com/matzegebbe/k8s-copycat/commit/505123e09b4e36c623be8a21b0bbdede121ea370))
* clarify registry credential guidance ([f8c04b2](https://github.com/matzegebbe/k8s-copycat/commit/f8c04b213df79aa4f2c7aec5bef8ba175fd19e83))

## [0.3.2](https://github.com/matzegebbe/k8s-copycat/compare/v0.3.1...v0.3.2) (2025-10-25)


### Bug Fixes

* **mirror:** normalize digest lookup and document flow ([4cedab6](https://github.com/matzegebbe/k8s-copycat/commit/4cedab638224dc450126d359ac94b259d0f7863b))
* normalize digest lookup and document mirroring flow ([5052079](https://github.com/matzegebbe/k8s-copycat/commit/50520793319cb68a260cdebd1f8f236f25645a88))

## [0.3.1](https://github.com/matzegebbe/k8s-copycat/compare/v0.3.0...v0.3.1) (2025-10-25)


### Refactoring

* rely on configured registry aliases ([e6c6690](https://github.com/matzegebbe/k8s-copycat/commit/e6c6690ff3e6610fe8ccbe97563383d7485e7df5))
* rely on configured registry aliases ([535676d](https://github.com/matzegebbe/k8s-copycat/commit/535676d48e7bb1f7e2157b200ca7a82d7689b76a))

## [0.3.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.6...v0.3.0) (2025-10-24)


### Features

* honor pod imageid for digest pulls ([967940a](https://github.com/matzegebbe/k8s-copycat/commit/967940a0388bd540dcd87bce2a274bc880ae5710))
* mirror digest pulls using pod image ids ([a63dc13](https://github.com/matzegebbe/k8s-copycat/commit/a63dc1392845f2205be9480521af50330b2a7ad2))


### Bug Fixes

* **mirror:** honor digest pull for manifest lists ([708bda2](https://github.com/matzegebbe/k8s-copycat/commit/708bda28e8036e83e936b01b826ff2e3fe9b27d0))


### Continuous Integration

* gate docker publish on commit tag ([08ca7ea](https://github.com/matzegebbe/k8s-copycat/commit/08ca7ea366ba1655ab81810cbc3fad347d54459d))


### Miscellaneous

* **deps:** bump github.com/aws/aws-sdk-go-v2/service/ecr ([95de82a](https://github.com/matzegebbe/k8s-copycat/commit/95de82a18d322dcf4ddb91ec2401ee8ba4d0d109))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/ecr from 1.50.7 to 1.51.0 ([56f764b](https://github.com/matzegebbe/k8s-copycat/commit/56f764bd9fd66d9bc00661cf958a2f9f1879e953))
* **deps:** bump the go-patch group with 2 updates ([36f6cb6](https://github.com/matzegebbe/k8s-copycat/commit/36f6cb67b07e20820c3299d2a0a1d29138dcb4c2))
* **deps:** bump the go-patch group with 2 updates ([c47df17](https://github.com/matzegebbe/k8s-copycat/commit/c47df17f719def87cea2acd5022560eceec6145b))

## [0.2.6](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.5...v0.2.6) (2025-10-23)


### Bug Fixes

* adjust registry push logging ([7522ec5](https://github.com/matzegebbe/k8s-copycat/commit/7522ec556dbf18d26cba47dcb161d240d84f48e1))
* **logging:** adjust push log verbosity ([e92e5eb](https://github.com/matzegebbe/k8s-copycat/commit/e92e5ebb163916e4045d25fd8d07abaa4fbd9165))
* rename registry exclusion configuration ([ebad7b1](https://github.com/matzegebbe/k8s-copycat/commit/ebad7b11608d552cf854c4b0cafb5277d6872fec))
* rename registry exclusion configuration ([ca22f7d](https://github.com/matzegebbe/k8s-copycat/commit/ca22f7d92a9a5368a7b196abeae74aac052aa5eb))


### Miscellaneous

* **deps:** bump actions/setup-go from 5 to 6 ([d66ce81](https://github.com/matzegebbe/k8s-copycat/commit/d66ce81f44e386e47386534715d59c21db177ef0))
* **deps:** bump actions/setup-go from 5 to 6 ([c6b4778](https://github.com/matzegebbe/k8s-copycat/commit/c6b47787b54bcca19b45e348d427a415b13eb0a7))
* **deps:** bump helm/kind-action from 1.7.0 to 1.12.0 ([09050ea](https://github.com/matzegebbe/k8s-copycat/commit/09050ea966e274c540c3916967a4de7de0a99bf1))
* **deps:** bump helm/kind-action from 1.7.0 to 1.12.0 ([42ff90d](https://github.com/matzegebbe/k8s-copycat/commit/42ff90dd47203e39a14ebc6f2f5bc3971219c8ad))
* **deps:** bump the go-patch group with 2 updates ([0d97f97](https://github.com/matzegebbe/k8s-copycat/commit/0d97f977da7608704a1972ea9cf902bd71c536da))
* **deps:** bump the go-patch group with 2 updates ([36cf003](https://github.com/matzegebbe/k8s-copycat/commit/36cf003fca3187bf3bec33c5ef1491fda5aa555e))

## [0.2.5](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.4...v0.2.5) (2025-10-19)


### Continuous Integration

* add token to release-please workflow configuration ([049442a](https://github.com/matzegebbe/k8s-copycat/commit/049442a4b6d0af110de0bccaf8b1d5a9c2d3c2b6))

## [0.2.4](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.3...v0.2.4) (2025-10-19)


### Bug Fixes

* Update tag pattern for release workflow vx.x.x ([1457870](https://github.com/matzegebbe/k8s-copycat/commit/145787086397af64f4e05faec86e555207f60d43))

## [0.2.3](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.2...v0.2.3) (2025-10-19)


### Continuous Integration

* release on tags ([a722aa9](https://github.com/matzegebbe/k8s-copycat/commit/a722aa9777e0175cb3ade3233b80378408a6aeb4))

## [0.2.2](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.1...v0.2.2) (2025-10-18)


### Miscellaneous

* migrate release process to release-please ([22a0585](https://github.com/matzegebbe/k8s-copycat/commit/22a0585e4edf81d31cbbd58c78159f60390d9b41))
* migrate releases to release-please ([43c3e1c](https://github.com/matzegebbe/k8s-copycat/commit/43c3e1c162a35caf91cadaa62ee6ec0d6d85387f))

## [0.2.1](https://github.com/matzegebbe/k8s-copycat/compare/v0.2.0...v0.2.1) (2025-10-18)


### Bug Fixes

* also create release on api ([a4725b7](https://github.com/matzegebbe/k8s-copycat/commit/a4725b787f4b21f3c9013afce8ead1faad4eb303))
* also create release on api  ([bb6485c](https://github.com/matzegebbe/k8s-copycat/commit/bb6485c93138ac5360709461367c1478a898d81e))

## [0.2.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.1.0...v0.2.0) (2025-10-18)


### Features

* clarify dry pull logging ([48ad2c4](https://github.com/matzegebbe/k8s-copycat/commit/48ad2c446ce9edf5712920ea937ab310b37a9154))

## [0.1.0](https://github.com/matzegebbe/k8s-copycat/compare/v0.0.13...v0.1.0) (2025-10-18)


### Features

* continue force reconcile after partial failures ([e0a54a0](https://github.com/matzegebbe/k8s-copycat/commit/e0a54a00d112c5e3965b045921bdfaedd9ed9cf3))


### Bug Fixes

* add mandatory settings to manifest and log 200 in ci action ([3f02b19](https://github.com/matzegebbe/k8s-copycat/commit/3f02b1988e43b51f249dc17ffed78815d8e47b91))
