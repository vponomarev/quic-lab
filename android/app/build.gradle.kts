plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }
val packageMdmEditor by tasks.registering(Copy::class) {
    from("../../internal/admin") { include("mdm_editor.js", "mdm_phone.html") }
    into(layout.buildDirectory.dir("generated/mdmAssets/mdm"))
}
android {
    sourceSets.getByName("main").assets.srcDir(layout.buildDirectory.dir("generated/mdmAssets"))
    namespace = "ru.vpnc.quiclab"
    compileSdk = 35
    buildFeatures { buildConfig = true }
    defaultConfig {
        applicationId = "ru.vpnc.quiclab"
        minSdk = 30
        targetSdk = 35
        versionCode = 77
        versionName = "0.9.5-pre.12"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
}
tasks.named("preBuild").configure { dependsOn(packageMdmEditor) }
dependencies {
    // CaptureManager uses ContextCompat and ActivityCompat at runtime.
    implementation("androidx.core:core:1.16.0")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.google.zxing:core:3.5.3")
    implementation(files("libs/quiclab.aar"))
    testImplementation("junit:junit:4.13.2")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
}
