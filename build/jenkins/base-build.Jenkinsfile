//
// Jenkins pipeline DSL for a "basic build" of this repository.  This is
// intended to exercise the main ways one can build this repo.  It is designed
// to turn around results fairly quickly and as such it is not intended to
// exhaustively test every part of the Makefile infrastructure-- that can be
// done by other jobs which repeatedly build and clobber with different
// options.
//
// More information about the Jenkins pipeline DSL can be found at:
//  - https://jenkins.io/doc/book/pipeline/
//  - https://jenkins.io/doc/book/pipeline/development/
//
// To lint this file as you make changes:
//  - ./lint-Jenkinsfile.sh base-build.Jenkinsfile
//
// n.b. that there are two related DSLs-- a "scripted" one and a "declarative"
// one.  We are using "declarative" for now but this is not dogmatic.
//
pipeline {
    agent any
    environment {
        GOROOT = '/opt/net.b10e/go-1.12.15'
        DOWNLOAD_CACHEDIR = '/ex1/product-dl-cache'
        GCS_KEY_ARTIFACT = credentials('bg-artifact-uploader')
        GCS_KEY_SYSROOT = credentials('sysroot-uploader')
    }
    stages {
        stage('build-amd64') {
            steps {
                sh 'make'
                // Incremental 'make' after above should do nothing
                sh 'make -q'

                sh 'make install'
                // Incremental 'make install' after above should do nothing
                sh 'make -q install'

                sh 'make packages'
                archiveArtifacts '*_amd64.deb'
            }
        }
        stage('build-arm') {
            environment {
                GOARCH = 'arm'
                GOARM = '7'
            }
            steps {
                sh 'make packages'
                archiveArtifacts '*_armhf.deb'
            }
        }
        stage('build-arm-openwrt') {
            environment {
                GOARCH = 'arm'
                GOARM = '7'
            }
            steps {
                sh 'make packages DISTRO=openwrt'
                archiveArtifacts '*_arm_*.ipk'
            }
        }
        stage('test') {
            steps {
                sh 'make test coverage'
                archiveArtifacts 'coverage/coverage.html'
            }
        }
        stage('checks') {
            steps {
                sh 'make check-go'
                sh 'make check-dirty'
            }
        }
        stage('doc') {
            steps {
                sh 'make doc-check'
                sh 'make doc'
                // Incremental 'make doc' after above should do nothing
                sh 'make -q doc'
                archiveArtifacts 'doc/output/*.pdf'
            }
        }
    }
    post {
        success {
            script {
                if (env.JOB_BASE_NAME == 'postcommit-PS') {
                    sh 'make packages-upload'
                }
            }
        }
        // "cleanup" clauses are guaranteed to run last
        cleanup {
            phabNotify()
        }
    }
}
