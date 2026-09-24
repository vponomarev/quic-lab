param([switch]$BuildApk)

$ErrorActionPreference = 'Stop'
$labRoot = Split-Path -Parent $PSScriptRoot
$previousJavaHome = $env:JAVA_HOME
$previousPath = $env:PATH
Push-Location $labRoot
try {
    if (-not $env:JAVA_HOME) { throw 'Set JAVA_HOME to JDK 17 (not the Java 25 bundled with current Android Studio).' }
    $env:PATH = "$env:JAVA_HOME/bin;$env:PATH"
    if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = "$env:LOCALAPPDATA/Android/Sdk" }
    if (-not $env:ANDROID_NDK_HOME) { $env:ANDROID_NDK_HOME = "$env:ANDROID_HOME/ndk/27.2.12479018" }
    if (-not (Test-Path "$env:ANDROID_HOME/platforms/android-35/android.jar")) { throw 'Install Android SDK Platform 35.' }
    if (-not (Test-Path "$env:ANDROID_NDK_HOME/source.properties")) { throw 'Install NDK 27.2.12479018 or set ANDROID_NDK_HOME.' }
    New-Item -ItemType Directory -Force 'bin', 'android/app/libs' | Out-Null
    go build -o bin/gomobile.exe golang.org/x/mobile/cmd/gomobile
    if ($LASTEXITCODE) { throw 'gomobile build failed' }
    go build -o bin/gobind.exe golang.org/x/mobile/cmd/gobind
    if ($LASTEXITCODE) { throw 'gobind build failed' }
    $env:PATH = "$labRoot/bin;$env:PATH"
    # NDK r27 needs explicit ELF alignment for Android devices with 16 KB pages.
    & ./bin/gomobile.exe bind -trimpath '-target=android/arm64,android/amd64' -androidapi=31 '-ldflags=-s -w -extldflags=-Wl,-z,max-page-size=16384,-z,common-page-size=16384' -o android/app/libs/quiclab.aar ./mobile
    if ($LASTEXITCODE) { throw 'AAR build failed' }
    if ($BuildApk) {
        & ./android/gradlew.bat -p android assembleDebug lintDebug --console=plain
        if ($LASTEXITCODE) { throw 'APK build or lint failed' }
        Write-Host "APK: $labRoot/android/app/build/outputs/apk/debug/app-debug.apk"
    } else {
        Write-Host 'AAR ready. Open android/ in Android Studio (Gradle JDK 17) or rerun with -BuildApk.'
    }
} finally {
    $env:JAVA_HOME = $previousJavaHome
    $env:PATH = $previousPath
    Pop-Location
}
