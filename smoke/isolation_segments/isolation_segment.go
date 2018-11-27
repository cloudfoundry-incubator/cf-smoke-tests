package isolation_segments

import (
	"fmt"
	"io/ioutil"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"

	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/generator"
	"github.com/cloudfoundry-incubator/cf-test-helpers/workflowhelpers"
)

const (
	sharedIsolationSegmentGUID = "933b4c58-120b-499a-b85d-4b6fc9e2903b"
	binaryHi                   = "Hello from a binary"
	binaryAppBitsPath          = "../../assets/binary"
)

var _ = Describe("RoutingIsolationSegments", func() {
	var (
		appsDomain                 string
		orgGUID, orgName           string
		spaceGUID, spaceName       string
		isoSpaceGUID, isoSpaceName string
		isoSegGUID                 string
		isoSegName, isoSegDomain   string
		appName                    string
	)

	BeforeEach(func() {
		// New up a organization since we will be assigning isolation segments.
		// This has a potential to cause other tests to fail if running in parallel mode.
		if testConfig.EnableIsolationSegmentTests != true {
			Skip("Skipping because EnableIsolationSegmentTests flag is set to false")
		}

		appsDomain = testConfig.GetAppsDomains()
		orgName = testSetup.RegularUserContext().Org
		orgGUID = GetOrgGUIDFromName(orgName, testConfig.GetDefaultTimeout())
		spaceName = testSetup.RegularUserContext().Space
		spaceGUID = GetSpaceGUIDFromName(spaceName, testConfig.GetDefaultTimeout())
		isoSpaceName = spaceName
		isoSpaceGUID = spaceGUID
		appName = generator.PrefixedRandomName("SMOKES", "APP")

		isoSegName = testConfig.GetIsolationSegmentName()
		isoSegDomain = testConfig.GetIsolationSegmentDomain()

		if testConfig.GetUseExistingOrganization() && testConfig.GetUseExistingSpace() {
			if !OrgEntitledToIsolationSegment(orgGUID, isoSegName, testConfig.GetDefaultTimeout()) {
				Fail(fmt.Sprintf("Pre-existing org %s is not entitled to isolation segment %s", orgName, isoSegName))
			}
			isoSpaceName = testConfig.GetIsolationSegmentSpace()
			isoSpaceGUID = GetSpaceGUIDFromName(isoSpaceName, testConfig.GetDefaultTimeout())
			if !IsolationSegmentAssignedToSpace(isoSpaceGUID, testConfig.GetDefaultTimeout()) {
				Fail(fmt.Sprintf("No isolation segment assigned  to pre-existing space %s", isoSpaceName))
			}
		}

		session := cf.Cf("curl", fmt.Sprintf("/v3/organizations?names=%s", orgName))
		bytes := session.Wait(testConfig.GetDefaultTimeout()).Out.Contents()
		orgGUID = GetGUIDFromResponse(bytes)
	})

	AfterEach(func() {
		if testConfig.Cleanup {
			Expect(cf.Cf("delete", appName, "-f", "-r").Wait(testConfig.GetDefaultTimeout())).To(Exit(0))
		}
	})

	Context("When an app is pushed to a space assigned the shared isolation segment", func() {
		BeforeEach(func() {
			if !testConfig.GetUseExistingOrganization() && !testConfig.GetUseExistingSpace() {
				workflowhelpers.AsUser(testSetup.AdminUserContext(), testSetup.ShortTimeout(), func() {
					EntitleOrgToIsolationSegment(orgGUID, sharedIsolationSegmentGUID, testConfig.GetDefaultTimeout())
					AssignIsolationSegmentToSpace(spaceGUID, sharedIsolationSegmentGUID, testConfig.GetDefaultTimeout())
				})

				testSetup.RegularUserContext().Login()
				testSetup.RegularUserContext().TargetSpace()
			}

			Eventually(cf.Cf(
				"push", appName,
				"-p", binaryAppBitsPath,
				"-b", "binary_buildpack",
				"-d", appsDomain,
				"-c", "./app"),
				testConfig.GetPushTimeout()).Should(Exit(0))
		})

		It("is reachable from the shared router", func() {
			resp := SendRequestWithSpoofedHeader(fmt.Sprintf("%s.%s", appName, appsDomain), appsDomain)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(200))
			htmlData, err := ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(htmlData)).To(ContainSubstring(binaryHi))
		})

		It("is not reachable from the isolation segment router", func() {
			//send a request to app in the shared domain, but through the isolation segment router
			resp := SendRequestWithSpoofedHeader(fmt.Sprintf("%s.%s", appName, appsDomain), isoSegDomain)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(404))
		})
	})

	Context("When an app is pushed to a space that has been assigned an Isolation Segment", func() {
		var appName string

		BeforeEach(func() {
			isoSegGUID = GetIsolationSegmentGUID(isoSegName, testConfig.GetDefaultTimeout())
			if !testConfig.GetUseExistingOrganization() {
				EntitleOrgToIsolationSegment(orgGUID, isoSegGUID, testConfig.GetDefaultTimeout())
			}

			if !testConfig.GetUseExistingSpace() {
				AssignIsolationSegmentToSpace(isoSpaceGUID, isoSegGUID, testConfig.GetDefaultTimeout())
			}
			appName = generator.PrefixedRandomName("SMOKES", "APP")
			Eventually(cf.Cf("target", "-s", isoSpaceName), testConfig.GetDefaultTimeout()).Should(Exit(0))
			Eventually(cf.Cf(
				"push", appName,
				"-p", binaryAppBitsPath,
				"-b", "binary_buildpack",
				"-d", isoSegDomain,
				"-c", "./app"),
				testConfig.GetPushTimeout()).Should(Exit(0))
		})

		It("the app is reachable from the isolated router", func() {
			resp := SendRequestWithSpoofedHeader(fmt.Sprintf("%s.%s", appName, isoSegDomain), isoSegDomain)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(200))
			htmlData, err := ioutil.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(htmlData)).To(ContainSubstring(binaryHi))
		})

		It("the app is not reachable from the shared router", func() {

			resp := SendRequestWithSpoofedHeader(fmt.Sprintf("%s.%s", appName, isoSegDomain), appsDomain)
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(404))
		})
	})
})
