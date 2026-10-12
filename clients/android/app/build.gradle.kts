import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val version = rootDir.resolve("../../VERSION").readText().trim()

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
        versionCode = 1
        versionName = version
    }

    signingConfigs {
        getByName("debug") {
            System.getenv("GOTOME_DEBUG_KEYSTORE")?.let { storeFile = file(it) }
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
