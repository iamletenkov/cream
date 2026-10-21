# Changelog

## [1.3.0](https://github.com/iamletenkov/cream/compare/v1.2.0...v1.3.0) (2026-10-21)


### Добавлено

* **openwrt:** семейство openwrt — раскладка /opt/cream/var, configure из init-скриптов procd ([#37](https://github.com/iamletenkov/cream/issues/37)) ([dc9023c](https://github.com/iamletenkov/cream/commit/dc9023c1cdeefb859b30751710142b2468867c0f))

## [1.2.0](https://github.com/iamletenkov/cream/compare/v1.1.4...v1.2.0) (2026-10-21)


### Добавлено

* **check:** шаг install без обязательного ключа scope: build — 64 config.build_required ([1dd5fe1](https://github.com/iamletenkov/cream/commit/1dd5fe1f8ed65d6029745f006c4fe846c82500c9))
* **cli:** cream check — проверка строк cream образа до сборки (§10.3) ([#31](https://github.com/iamletenkov/cream/issues/31)) ([1509982](https://github.com/iamletenkov/cream/commit/15099823b5186cb92c94ba2a9504189e2cc6a5d3))
* **cli:** команды hold и unhold закрепляют версию расширения ([0e62232](https://github.com/iamletenkov/cream/commit/0e622324117e7069c91e70add8a684a08feb67e6))
* **engine:** cream configure --wait отдаёт исход юнита, код 20 снимает отметку ([1dd5fe1](https://github.com/iamletenkov/cream/commit/1dd5fe1f8ed65d6029745f006c4fe846c82500c9))
* **engine:** install, upgrade и remove в фазе build из источника ([#28](https://github.com/iamletenkov/cream/issues/28)) ([3e7c992](https://github.com/iamletenkov/cream/commit/3e7c992c7666c39f53f32728d13ee0a4f26c3cbb))
* **engine:** scope: build — значение только в окружении хука install/upgrade ([#30](https://github.com/iamletenkov/cream/issues/30)) ([6ba7278](https://github.com/iamletenkov/cream/commit/6ba727889c73739d467d6e5445a9ae6a5e7f36b6))
* **engine:** код 20 хука configure — «пропущено», не сбой ([#33](https://github.com/iamletenkov/cream/issues/33)) ([66942a7](https://github.com/iamletenkov/cream/commit/66942a7824b0eb1682b447c095b05b8a88e922db))
* **engine:** отметка выполнения configure — на загрузке хук идёт, только если что-то изменилось ([#32](https://github.com/iamletenkov/cream/issues/32)) ([40b7034](https://github.com/iamletenkov/cream/commit/40b70345a7ad8cb2f4e6c801bf9e3b91d41ce5d8))
* **engine:** фаза install/upgrade/remove определяется сама (§10.1) ([#27](https://github.com/iamletenkov/cream/issues/27)) ([f8fd6c6](https://github.com/iamletenkov/cream/commit/f8fd6c6370b7681eb9a643521a95898b38f7603c))
* **manifest:** поля every_boot и scope: build требуют cream &gt;=1.2.0 (ADR-0004) ([bd81d06](https://github.com/iamletenkov/cream/commit/bd81d0675e6fccc2d21035c36ed781cd294ec2c1))
* **state:** /etc/image-labels.d/cream.json — состав образа для меток OCI ([#29](https://github.com/iamletenkov/cream/issues/29)) ([4d7d3d2](https://github.com/iamletenkov/cream/commit/4d7d3d2382a50de08f330898095c8b908f2df5f0))
* **state:** предупреждение labels.too_large при cream.extensions больше 8 КиБ ([1dd5fe1](https://github.com/iamletenkov/cream/commit/1dd5fe1f8ed65d6029745f006c4fe846c82500c9))
* **upgrade:** план до действий, --dry-run, --accept-risk &lt;ext&gt;, -y, --fail-fast, частичный исход с кодом худшего ([0e62232](https://github.com/iamletenkov/cream/commit/0e622324117e7069c91e70add8a684a08feb67e6))


### Исправлено

* **upgrade:** расширение с targets.live: confirm без новой версии не роняет upgrade --all кодом 68 ([0e62232](https://github.com/iamletenkov/cream/commit/0e622324117e7069c91e70add8a684a08feb67e6))

## [1.1.4](https://github.com/iamletenkov/cream/compare/v1.1.3...v1.1.4) (2026-10-20)


### Исправлено

* **catalog:** unit_lock: неполный анализ по расширению, «возможен тупик», service только как команда ([#13](https://github.com/iamletenkov/cream/issues/13)) ([435c68d](https://github.com/iamletenkov/cream/commit/435c68d928f4f1c78ee0dbc12f4db9d18c2406b1))

## [1.1.3](https://github.com/iamletenkov/cream/compare/v1.1.2...v1.1.3) (2026-10-20)


### Исправлено

* **catalog:** validate.unit_lock анализирует только исполняемое на машине ([#11](https://github.com/iamletenkov/cream/issues/11)) ([98acf6b](https://github.com/iamletenkov/cream/commit/98acf6bb4fbddd14ae2c440a9fcec767ccf8eeee))

## [1.1.2](https://github.com/iamletenkov/cream/compare/v1.1.1...v1.1.2) (2026-10-19)


### Исправлено

* **catalog:** validate.unit_lock предупреждает только о реальном тупике §7.3 ([#9](https://github.com/iamletenkov/cream/issues/9)) ([be9944e](https://github.com/iamletenkov/cream/commit/be9944ea3d31c0e2ee98583d0b2f7b33f2e9aa12))

## [1.1.1](https://github.com/iamletenkov/cream/compare/v1.1.0...v1.1.1) (2026-10-19)


### Исправлено

* cream 1.1.1 — стадия cloud-user без cloud-init, system init без лишней блокировки ([ddfbd3d](https://github.com/iamletenkov/cream/commit/ddfbd3d6d4ee87bf597f478e41467750901a64c4))

## [1.1.0](https://github.com/iamletenkov/cream/compare/v1.0.0...v1.1.0) (2026-10-19)


### Добавлено

* cream 1.1 — стадии configure, resolve --installed, граф каталога ([#5](https://github.com/iamletenkov/cream/issues/5)) ([876952c](https://github.com/iamletenkov/cream/commit/876952c4ba26c747092dd9f05e9328b8a0decc3e))

## 1.0.0 (2026-10-19)


### Добавлено

* cream 1.0 — формат cream/v1 и исполнитель ([f770f72](https://github.com/iamletenkov/cream/commit/f770f72ac931265c187f35e32069c045624b30af))


### Исправлено

* **claude:** allow-список без произвольного docker run и make ([5505500](https://github.com/iamletenkov/cream/commit/5505500ff53d112d504ad5d7bbc2ff4a9a19cd39))
* **claude:** хук и allow-список закрывают git-флаги с исполнением команд ([f2170d8](https://github.com/iamletenkov/cream/commit/f2170d8aed9a5bbdae5b4bad8502b54704f07956))
* **claude:** хук отказывает обходу git-хуков, gitleaks строже и в CI ([6ced38b](https://github.com/iamletenkov/cream/commit/6ced38b7811fe8d12444ac54e9450b99a0b86ce4))
