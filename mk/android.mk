# The Tolino companion app (clients/android). Gradle and the Android SDK run in
# a container like every other toolchain. Gradle's caches and the debug signing
# key live under .cache/android, so a build installs over the one before it:
# left to itself, the Android plugin makes a new key in the container's home
# on every run.
ANDROID_BASE ?= ghcr.io/cirruslabs/android-sdk:35@sha256:c724009e305b4607157287624033ab97f319af44c244bfc9f73b6293f3bb01b9
ANDROID_TOOLCHAIN_FILE := $(ROOT)/deploy/android.Dockerfile
ANDROID_IMAGE ?= gotome-android:$(shell printf '%s' '$(ANDROID_BASE)' | cat - '$(ANDROID_TOOLCHAIN_FILE)' | cksum | cut -d' ' -f1)
ANDROID_CACHE := $(ROOT)/.cache/android
ANDROID_APK := $(ROOT)/clients/android/app/build/outputs/apk/debug/app-debug.apk
ANDROID_PACKAGE := io.github.praetorianer777.gotome.tolino
ANDROID_KEYSTORE := $(ANDROID_CACHE)/debug.keystore

DOCKER_GRADLE = docker run --rm \
	-u $(UID_GID) \
	-v $(ROOT):/work \
	-v $(ANDROID_CACHE):/cache \
	-e HOME=/cache/home \
	-e GRADLE_USER_HOME=/cache/gradle \
	-e GOTOME_DEBUG_KEYSTORE=/cache/debug.keystore \
	-w /work/clients/android $(ANDROID_IMAGE) \
	gradle --no-daemon --console=plain --warning-mode=fail

$(ANDROID_CACHE):
	@mkdir -p $@/home $@/gradle

# The key Android's own debug builds use, with its well-known passwords.
$(ANDROID_KEYSTORE): | android-toolchain $(ANDROID_CACHE)
	docker run --rm -u $(UID_GID) -v $(ANDROID_CACHE):/cache $(ANDROID_IMAGE) \
		keytool -genkeypair -keystore /cache/debug.keystore -storepass android \
		-alias androiddebugkey -keypass android -keyalg RSA -keysize 2048 -validity 10000 \
		-dname "CN=Android Debug,O=Android,C=US"

.PHONY: android-toolchain
android-toolchain:
	@docker image inspect $(ANDROID_IMAGE) >/dev/null 2>&1 || \
		docker build --quiet --build-arg ANDROID_BASE=$(ANDROID_BASE) -t $(ANDROID_IMAGE) - < $(ANDROID_TOOLCHAIN_FILE)

.PHONY: check-android
check-android: android-toolchain $(ANDROID_KEYSTORE) | $(ANDROID_CACHE) ## The companion app's gate: unit tests and a debug build
	$(DOCKER_GRADLE) testDebugUnitTest assembleDebug

.PHONY: android-apk
android-apk: android-toolchain $(ANDROID_KEYSTORE) | $(ANDROID_CACHE) ## Build the companion app's APK
	$(DOCKER_GRADLE) assembleDebug
	@echo "$(ANDROID_APK)"

# adb runs on the host, where the Tolino is plugged in. READ_LOGS lets the app
# see the reader's page turns; it can only be granted over adb. Storage is
# granted too, so the setup asks for nothing: reading as well as writing, as
# without both Android 8.1 starts the app with a view of storage it cannot
# write to. The jump to a page read elsewhere is an accessibility service,
# added to those already on, not in their place.
.PHONY: android-install
android-install: android-apk ## Install the companion app on the Tolino over adb and grant it the log
	adb install -r $(ANDROID_APK)
	adb shell pm grant $(ANDROID_PACKAGE) android.permission.READ_LOGS
	adb shell pm grant $(ANDROID_PACKAGE) android.permission.READ_EXTERNAL_STORAGE
	adb shell pm grant $(ANDROID_PACKAGE) android.permission.WRITE_EXTERNAL_STORAGE
	adb shell am force-stop $(ANDROID_PACKAGE)
	adb shell 'on=$$(settings get secure enabled_accessibility_services); \
		case "$$on" in *$(ANDROID_PACKAGE)/*) ;; null|"") on=$(ANDROID_PACKAGE)/.JumpService ;; *) on="$$on:$(ANDROID_PACKAGE)/.JumpService" ;; esac; \
		settings put secure enabled_accessibility_services "$$on"; settings put secure accessibility_enabled 1'
	adb shell am start -n $(ANDROID_PACKAGE)/.StoreActivity
