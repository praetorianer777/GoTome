import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val version = rootDir.resolve("../../VERSION").readText().trim()

// Android installs an update only over a lower versionCode: 0.1.0 is 100, 1.2.3 is 10203.
val code = version.split(".").map { it.toInt() }.let { (major, minor, patch) -> major * 10000 + minor * 100 + patch }
    .coerceAtLeast(1)

// The release key is the owner's: release.yml hands it in from the repository's
// secrets. Without it a release is not built at all rather than signed with
// another key, which no installed release would take as its update.
val releaseKeystore = System.getenv("GOTOME_RELEASE_KEYSTORE")
val releasePassword = System.getenv("GOTOME_RELEASE_KEYSTORE_PASSWORD")

android {
    namespace = "io.github.praetorianer777.gotome.tolino"
    compileSdk = 35
    // The image carries this one; another would be downloaded into an SDK the
    // build cannot write to.
    buildToolsVersion = "35.0.0"

    defaultConfig {
        applicationId = "io.github.praetorianer777.gotome.tolino"
        // Tolinos from the Shine 4 on run Android 8.1. Targeting it keeps the
        // app to the rules of the system it runs on.
        minSdk = 26
        targetSdk = 27
        versionCode = code
        versionName = version
        // Where the app looks for a newer release; a test build may point it elsewhere.
        val releases = (project.findProperty("gotome.releases") as String?)
            ?: "https://api.github.com/repos/praetorianer777/GoTome/releases/latest"
        buildConfigField("String", "RELEASES", "\"$releases\"")
    }

    signingConfigs {
        getByName("debug") {
            System.getenv("GOTOME_DEBUG_KEYSTORE")?.let { storeFile = file(it) }
        }
        if (releaseKeystore != null) {
            create("release") {
                storeFile = file(releaseKeystore)
                storePassword = releasePassword
                keyAlias = "gotome-tolino"
                keyPassword = releasePassword
            }
        }
    }

    buildTypes {
        getByName("release") {
            signingConfig = signingConfigs.findByName("release")
        }
    }

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    lint {
        disable += "ExpiredTargetSdkVersion"
        // Lint's own libraries would be fetched for every release build.
        checkReleaseBuilds = false
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

dependencies {
    // android.jar holds only stubs of org.json, so the JSON tests need the real one.
    testImplementation("org.json:json:20240303")
    testImplementation("junit:junit:4.13.2")
}

gradle.taskGraph.whenReady {
    if (allTasks.any { it.name == "assembleRelease" } && releaseKeystore == null) {
        throw GradleException("A release needs the release key: GOTOME_RELEASE_KEYSTORE and GOTOME_RELEASE_KEYSTORE_PASSWORD")
    }
}
