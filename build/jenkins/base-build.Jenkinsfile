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
    stages {
        stage('build') {
            steps {
                sh 'source env.sh && make'
                // Incremental 'make' after above should do nothing
                sh 'source env.sh && make -q'

                sh 'source env.sh && make util'
                // Incremental 'make util' after above should do nothing
                sh 'source env.sh && make -q util'

                sh 'source env.sh && make install'
                // Incremental 'make install' after above should do nothing
                sh 'source env.sh && make -q install'
            }
        }
        stage('packaging') {
            steps {
                sh 'source env.sh && make packages'
                archiveArtifacts '*.deb'
            }
        }
        stage('test') {
            steps {
                sh 'source env.sh && make test && make coverage'
            }
        }
        stage('checks') {
            steps {
                sh 'source env.sh && make vet-go && make lint-go'
            }
        }
    }
}
