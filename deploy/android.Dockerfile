# The image the Tolino companion app is built and tested in (mk/android.mk):
# the Android SDK of the base image and a Gradle distribution checked against
# its published checksum. Built without a context, so the only input is this
# file.
ARG ANDROID_BASE
FROM ${ANDROID_BASE}
ARG GRADLE_VERSION=8.11.1
ARG GRADLE_SHA256=f397b287023acdba1e9f6fc5ea72d22dd63669d59ed4a289a29b1a76eee151c6
RUN curl -fsSL -o /tmp/gradle.zip "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" && \
    echo "${GRADLE_SHA256}  /tmp/gradle.zip" | sha256sum -c - && \
    unzip -q /tmp/gradle.zip -d /opt && \
    rm /tmp/gradle.zip && \
    ln -s "/opt/gradle-${GRADLE_VERSION}/bin/gradle" /usr/local/bin/gradle
