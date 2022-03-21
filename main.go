package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gocolly/colly"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"gopkg.in/ezzarghili/recaptcha-go.v4"

	"github.com/jufabeck2202/piScraper/adaptors"
	"github.com/jufabeck2202/piScraper/messaging"
	"github.com/jufabeck2202/piScraper/messaging/types"
	"github.com/jufabeck2202/piScraper/utils"
)

var websites = utils.Websites{}

var alertManager = messaging.NewAlerts()

type AddTask struct {
	Tasks  []types.AlertTask `json:"tasks"`
	Capcha string            `json:"captcha"`
}


func main() {


	err := godotenv.Load(".env")

	if err != nil {
		fmt.Println("No .env file found")
	}
	captcha, _ := recaptcha.NewReCAPTCHA(os.Getenv("RECAPTCHA_SECRET"), recaptcha.V3, 10*time.Second) // for v3 API use https://g.co/recaptcha/v3 (apperently the same admin UI at the time of writing)
	go startScraper()

	go messaging.Init()

	app := fiber.New()

	// Or extend your config for customization
	app.Use(cors.New(cors.Config{
		AllowOrigins: "http://localhost:3000, http://localhost:3001, " + os.Getenv("HOST_URL"),
		AllowHeaders: "Origin, Content-Type, Accept",
	}))
	app.Static("/", "./frontend/build")

	app.Get("/api/v1/status", func(c *fiber.Ctx) error {

		return c.JSON(websites.GetList())
	})

	app.Post("/api/v1/alert", func(c *fiber.Ctx) error {
		// Create new Book struct
		addTasks := &AddTask{}

		// Check, if received JSON data is valid.
		if err := c.BodyParser(addTasks); err != nil {
			// Return status 400 and error message.
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}
		err := captcha.Verify(addTasks.Capcha)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": true,
				"msg":   err.Error(),
			})
		}
		//check if the task is valid
		for _, t := range addTasks.Tasks {
			if t.Recipient.Pushover == "" && t.Recipient.Webhook == "" && t.Recipient.Email == "" || t.Destination > 3 || t.Destination < 1 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": true,
					"msg":   "invalid task structure",
				})
			}
		}
		//Add task to alert
		for _, t := range addTasks.Tasks {
			alertManager.AddAlert(t.Website.URL, messaging.Task{t.Recipient, t.Destination})
		}
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"error": false,
			"msg":   "task added",
		})
	})

	app.Delete("/api/v1/alert", func(c *fiber.Ctx) error {

		return c.JSON(websites.GetList())
	})

	app.Listen(":3001")
}

func mainPage(c *fiber.Ctx) error {
	return c.Render("mainpage", fiber.Map{})
}

func startScraper() {
	websites.Init()
	c := cron.New()
	searchPi(true)
	c.AddFunc("* * * * *", func() {
		searchPi(false)
	})
	c.Start()
}
func searchPi(firstRun bool) {
	adaptorsList := make([]adaptors.Adaptor, 0)
	websites.Load()
	c := colly.NewCollector(
		colly.CacheDir("./cache"),
		colly.Async(true),
	)
	//
	adaptorsList = append(adaptorsList, adaptors.NewBechtle(c), adaptors.NewRappishop(c), adaptors.NewOkdo(c), adaptors.NewBerryBase(c), adaptors.NewSemaf(c), adaptors.NewBuyZero(c), adaptors.NewELV(c), adaptors.NewWelectron(c), adaptors.NewPishop(c), adaptors.NewFunk24(c), adaptors.NewReichelt(c))
	for _, site := range adaptorsList {
		site.Run(websites)
	}

	for _, site := range adaptorsList {
		site.Wait()
	}

	if !firstRun {
		changes := websites.CheckForChanges()
		if len(changes) > 0 {
			scheduleUpdates(changes)
		}
		fmt.Println("no changes")
	}
	websites.Save()
}

func scheduleUpdates(websites []utils.Website) {
	tasksToSchedule := []types.AlertTask{}

	for _, w := range websites {
		alert := alertManager.LoadAlerts(w.URL)
		for _, t := range alert {
			tasksToSchedule = append(tasksToSchedule, types.AlertTask{w, t.Recipient, t.Destination})
			fmt.Println("scheduling update for ", w.URL)
		}
	}
	fmt.Println("Found websites to update: ", len(tasksToSchedule))
	messaging.AddToQueue(tasksToSchedule)
}
