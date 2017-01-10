package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/bwmarrin/discordgo"
	"github.com/dustin/go-humanize"
	redis "gopkg.in/redis.v3"
)

var (
	// discordgo session
	discord *discordgo.Session

	// Redis client connection (used for stats)
	rcli *redis.Client

	// Map of Guild id's to *Play channels, used for queuing and rate-limiting guilds
	queues map[string]chan *Play = make(map[string]chan *Play)

	// Sound encoding settings
	BITRATE        = 128
	MAX_QUEUE_SIZE = 6

	// Owner
	OWNER string
)

// Play represents an individual use of the !airhorn command
type Play struct {
	GuildID   string
	ChannelID string
	UserID    string
	Sound     *Sound

	// The next play to occur after this, only used for chaining sounds like anotha
	Next *Play

	// If true, this was a forced play using a specific airhorn sound name
	Forced bool
}

type SoundCollection struct {
	Prefix    string
	Commands  []string
	Sounds    []*Sound
	ChainWith *SoundCollection

	soundRange int
}

// Sound represents a sound clip
type Sound struct {
	Name string

	// Weight adjust how likely it is this song will play, higher = more likely
	Weight int

	// Delay (in milliseconds) for the bot to wait before sending the disconnect request
	PartDelay int

	// Buffer to store encoded PCM packets
	buffer [][]byte
}

// Array of all the sounds we have
var AIRHORN *SoundCollection = &SoundCollection{
	Prefix: "airhorn",
	Commands: []string{
		"!airhorn",
	},
	Sounds: []*Sound{
		createSound("default", 1000, 250),
		createSound("reverb", 800, 250),
		createSound("spam", 800, 0),
		createSound("tripletap", 800, 250),
		createSound("fourtap", 800, 250),
		createSound("distant", 500, 250),
		createSound("echo", 500, 250),
		createSound("clownfull", 250, 250),
		createSound("clownshort", 250, 250),
		createSound("clownspam", 250, 0),
		createSound("highfartlong", 200, 250),
		createSound("highfartshort", 200, 250),
		createSound("midshort", 100, 250),
		createSound("truck", 10, 250),
	},
}
var OVERWATCH *SoundCollection = &SoundCollection{
	Prefix: "owult",
	Commands: []string{
		"!overwatch",
		"!owult",
	},
	Sounds: []*Sound{
		//looking for sounds on
		//http://rpboyer15.github.io/sounds-of-overwatch/
		createSound("bastion", 1000, 250),
		createSound("dva_enemy", 1000, 250),
		createSound("dva_friendly", 1000, 250),
		createSound("genji_enemy", 1000, 250),
		createSound("genji_friendly", 1000, 250),
		createSound("hanzo_enemy", 1000, 250),
		createSound("hanzo_friendly", 1000, 250),
		createSound("junkrat_enemy", 1000, 250),
		createSound("junkrat_friendly", 1000, 250),
		createSound("lucio_friendly", 1000, 250),
		createSound("lucio_enemy", 1000, 250),
		createSound("mccree_enemy", 1000, 250),
		createSound("mccree_friendly", 1000, 250),
		createSound("mei_friendly", 1000, 250),
		// //there may be multiple mei friendly ult lines
		// //from this: https://www.reddit.com/r/Overwatch/comments/4fdw0z/is_that_ultimate_friendly_or_hostile/
		createSound("mei_enemy", 1000, 250),
		createSound("mercy_friendly", 1000, 250),
		createSound("mercy_friendly_devil", 1000, 250),
		createSound("mercy_friendly_valkyrie", 1000, 250),
		createSound("mercy_enemy", 1000, 250),
		createSound("pharah_enemy", 1000, 250),
		createSound("pharah_friendly", 1000, 250),
		createSound("reaper_enemy", 1000, 250), //not found
		createSound("reaper_friendly", 1000, 250),
		createSound("reinhardt", 1000, 250),
		createSound("roadhog_enemy", 1000, 250),
		createSound("roadhog_friendly", 1000, 250),
		createSound("76_enemy", 1000, 250), //consider shortening to s76, s:76?
		createSound("76_friendly", 1000, 250),
		createSound("symmetra_friendly", 1000, 250),
		createSound("symmetra_enemy", 1000, 250), //each hero has a line for when they see an enemy symmetra turret. not sure how to implement
		createSound("torbjorn", 1000, 250),
		createSound("tracer_enemy", 1000, 250),    //enemy line has variations. variations are an argument for splitting it up to be !owtracer, putting them in separate sound collections
		createSound("tracer_friendly", 1000, 250), //doesn't exist?
		createSound("widow_enemy", 1000, 250),     //consider shortening to widow?
		createSound("widow_friendly", 1000, 250),  //same as above
		createSound("zarya_enemy", 1000, 250),
		createSound("zarya_friendly", 1000, 250),
		createSound("zenyatta_enemy", 1000, 250),
		createSound("zenyatta_friendly", 1000, 250),

		createSound("dva_;)", 1000, 250), //should be in its own sound repository
		createSound("anyong", 1000, 250),
	},
}

var KHALED *SoundCollection = &SoundCollection{
	Prefix:    "another",
	ChainWith: AIRHORN,
	Commands: []string{
		"!anotha",
		"!anothaone",
	},
	Sounds: []*Sound{
		createSound("one", 1, 250),
		createSound("one_classic", 1, 250),
		createSound("one_echo", 1, 250),
	},
}

var CENA *SoundCollection = &SoundCollection{
	Prefix: "jc",
	Commands: []string{
		"!johncena",
		"!cena",
	},
	Sounds: []*Sound{
		createSound("airhorn", 1, 250),
		createSound("echo", 1, 250),
		createSound("full", 1, 250),
		createSound("jc", 1, 250),
		createSound("nameis", 1, 250),
		createSound("spam", 1, 250),
	},
}

var ETHAN *SoundCollection = &SoundCollection{
	Prefix: "ethan",
	Commands: []string{
		"!ethan",
		"!eb",
		"!ethanbradberry",
		"!h3h3",
	},
	Sounds: []*Sound{
		createSound("areyou_classic", 100, 250),
		createSound("areyou_condensed", 100, 250),
		createSound("areyou_crazy", 100, 250),
		createSound("areyou_ethan", 100, 250),
		createSound("classic", 100, 250),
		createSound("echo", 100, 250),
		createSound("high", 100, 250),
		createSound("slowandlow", 100, 250),
		createSound("cuts", 30, 250),
		createSound("beat", 30, 250),
		createSound("sodiepop", 1, 250),
	},
}

var COW *SoundCollection = &SoundCollection{
	Prefix: "cow",
	Commands: []string{
		"!stan",
		"!stanislav",
	},
	Sounds: []*Sound{
		createSound("herd", 10, 250),
		createSound("moo", 10, 250),
		createSound("x3", 1, 250),
	},
}

var BIRTHDAY *SoundCollection = &SoundCollection{
	Prefix: "birthday",
	Commands: []string{
		"!birthday",
		"!bday",
	},
	Sounds: []*Sound{
		createSound("horn", 50, 250),
		createSound("horn3", 30, 250),
		createSound("sadhorn", 25, 250),
		createSound("weakhorn", 25, 250),
	},
}

var ROODE *SoundCollection = &SoundCollection{
	Prefix: "roode",
	Commands: []string{
		"!roode",
	},
	Sounds: []*Sound{
		createSound("glorious", 100, 250),
		createSound("defend", 5, 250),
		createSound("victorious_full", 1, 250),
	},
}

var REVIVAL *SoundCollection = &SoundCollection{
	Prefix: "revival",
	Commands: []string{
		"!revival",
	},
	Sounds: []*Sound{
		createSound("we_go_hard", 100, 250),
		createSound("say_yeah", 25, 250),
	},
}

var STYLES *SoundCollection = &SoundCollection{
	Prefix: "styles",
	Commands: []string{
		"!styles",
		"!aj",
	},
	Sounds: []*Sound{
		createSound("gay_community", 100, 250),
	},
}

var DUMMY *SoundCollection = &SoundCollection{
	Prefix: "dummy",
	Commands: []string{
		"!dummy",
	},
	Sounds: []*Sound{
		createSound("yeah", 100, 250),
	},
}

var TOBI *SoundCollection = &SoundCollection{
	Prefix: "tobi",
	Commands: []string{
		"!tobi",
		"!tobiwan",
	},
	Sounds: []*Sound{
		createSound("disaster", 100, 250),
	},
}

var JONES *SoundCollection = &SoundCollection{
	Prefix: "jones",
	Commands: []string{
		"!jones",
		"!alexjones",
	},
	Sounds: []*Sound{
		createSound("kissing_goblins", 100, 250),
		createSound("kissing_goblins_full", 100, 250),
		createSound("in_bed_goblin", 100, 250),
		createSound("charging_goblins", 100, 250),
		createSound("pepsi_taste_test", 100, 250),
		createSound("1776", 100, 250),
		createSound("human", 100, 250),
		createSound("destroy_everything", 100, 250),
		createSound("hot_blood", 100, 250),
		createSound("have_children", 100, 250),
		createSound("gang_of_mustaches", 100, 250),
		createSound("sick_of_it", 100, 250),
		createSound("what_is_that_joke", 100, 250),
		createSound("what_is_venezula", 100, 250),
	},
}

var MUMMY *SoundCollection = &SoundCollection{
	Prefix: "mummy",
	Commands: []string{
		"!mummy",
	},
	Sounds: []*Sound{
		createSound("1", 100, 250),
		createSound("2", 100, 250),
		createSound("3", 100, 250),
		createSound("4", 100, 250),
		createSound("5", 100, 250),
		createSound("6", 100, 250),
		createSound("7", 100, 250),
		createSound("8", 100, 250),
	},
}

var COLLECTIONS []*SoundCollection = []*SoundCollection{
	AIRHORN,
	KHALED,
	CENA,
	ETHAN,
	COW,
	BIRTHDAY,
	OVERWATCH,
	ROODE,
	REVIVAL,
	STYLES,
	DUMMY,
	TOBI,
	JONES,
	MUMMY,
}

// Create a Sound struct
func createSound(Name string, Weight int, PartDelay int) *Sound {
	return &Sound{
		Name:      Name,
		Weight:    Weight,
		PartDelay: PartDelay,
		buffer:    make([][]byte, 0),
	}
}

func (sc *SoundCollection) Load() {
	for _, sound := range sc.Sounds {
		sc.soundRange += sound.Weight
		sound.Load(sc)
	}
}

func (s *SoundCollection) Random() *Sound {
	var (
		i      int
		number int = randomRange(0, s.soundRange)
	)

	for _, sound := range s.Sounds {
		i += sound.Weight

		if number < i {
			return sound
		}
	}
	return nil
}

// Load attempts to load an encoded sound file from disk
// DCA files are pre-computed sound files that are easy to send to Discord.
// If you would like to create your own DCA files, please use:
// https://github.com/nstafie/dca-rs
// eg: dca-rs --raw -i <input wav file> > <output file>
func (s *Sound) Load(c *SoundCollection) error {
	path := fmt.Sprintf("audio/%v_%v.dca", c.Prefix, s.Name)

	file, err := os.Open(path)

	if err != nil {
		fmt.Println("error opening dca file :", err)
		return err
	}

	var opuslen int16

	for {
		// read opus frame length from dca file
		err = binary.Read(file, binary.LittleEndian, &opuslen)

		// If this is the end of the file, just return
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}

		if err != nil {
			fmt.Println("error reading from dca file :", err)
			return err
		}

		// read encoded pcm from dca file
		InBuf := make([]byte, opuslen)
		err = binary.Read(file, binary.LittleEndian, &InBuf)

		// Should not be any end of file errors
		if err != nil {
			fmt.Println("error reading from dca file :", err)
			return err
		}

		// append encoded pcm data to the buffer
		s.buffer = append(s.buffer, InBuf)
	}
}

// Plays this sound over the specified VoiceConnection
func (s *Sound) Play(vc *discordgo.VoiceConnection) {
	vc.Speaking(true)
	defer vc.Speaking(false)

	for _, buff := range s.buffer {
		vc.OpusSend <- buff
	}
}

// Attempts to find the current users voice channel inside a given guild
func getCurrentVoiceChannel(user *discordgo.User, guild *discordgo.Guild) *discordgo.Channel {
	for _, vs := range guild.VoiceStates {
		if vs.UserID == user.ID {
			channel, _ := discord.State.Channel(vs.ChannelID)
			return channel
		}
	}
	return nil
}

// Returns a random integer between min and max
func randomRange(min, max int) int {
	rand.Seed(time.Now().UTC().UnixNano())
	return rand.Intn(max-min) + min
}

// Prepares a play
func createPlay(user *discordgo.User, guild *discordgo.Guild, coll *SoundCollection, sound *Sound) *Play {
	// Grab the users voice channel
	channel := getCurrentVoiceChannel(user, guild)
	if channel == nil {
		log.WithFields(log.Fields{
			"user":  user.ID,
			"guild": guild.ID,
		}).Warning("Failed to find channel to play sound in")
		return nil
	}

	// Create the play
	play := &Play{
		GuildID:   guild.ID,
		ChannelID: channel.ID,
		UserID:    user.ID,
		Sound:     sound,
		Forced:    true,
	}

	// If we didn't get passed a manual sound, generate a random one
	if play.Sound == nil {
		play.Sound = coll.Random()
		play.Forced = false
	}

	// If the collection is a chained one, set the next sound
	if coll.ChainWith != nil {
		play.Next = &Play{
			GuildID:   play.GuildID,
			ChannelID: play.ChannelID,
			UserID:    play.UserID,
			Sound:     coll.ChainWith.Random(),
			Forced:    play.Forced,
		}
	}

	return play
}

// Prepares and enqueues a play into the ratelimit/buffer guild queue
func enqueuePlay(user *discordgo.User, guild *discordgo.Guild, coll *SoundCollection, sound *Sound) {
	play := createPlay(user, guild, coll, sound)
	if play == nil {
		return
	}

	// Check if we already have a connection to this guild
	//   yes, this isn't threadsafe, but its "OK" 99% of the time
	_, exists := queues[guild.ID]

	if exists {
		if len(queues[guild.ID]) < MAX_QUEUE_SIZE {
			queues[guild.ID] <- play
		}
	} else {
		queues[guild.ID] = make(chan *Play, MAX_QUEUE_SIZE)
		playSound(play, nil)
	}
}

func trackSoundStats(play *Play) {
	if rcli == nil {
		return
	}

	_, err := rcli.Pipelined(func(pipe *redis.Pipeline) error {
		var baseChar string

		if play.Forced {
			baseChar = "f"
		} else {
			baseChar = "a"
		}

		base := fmt.Sprintf("airhorn:%s", baseChar)
		pipe.Incr("airhorn:total")
		pipe.Incr(fmt.Sprintf("%s:total", base))
		pipe.Incr(fmt.Sprintf("%s:sound:%s", base, play.Sound.Name))
		pipe.Incr(fmt.Sprintf("%s:user:%s:sound:%s", base, play.UserID, play.Sound.Name))
		pipe.Incr(fmt.Sprintf("%s:guild:%s:sound:%s", base, play.GuildID, play.Sound.Name))
		pipe.Incr(fmt.Sprintf("%s:guild:%s:chan:%s:sound:%s", base, play.GuildID, play.ChannelID, play.Sound.Name))
		pipe.SAdd(fmt.Sprintf("%s:users", base), play.UserID)
		pipe.SAdd(fmt.Sprintf("%s:guilds", base), play.GuildID)
		pipe.SAdd(fmt.Sprintf("%s:channels", base), play.ChannelID)
		return nil
	})

	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Warning("Failed to track stats in redis")
	}
}

// Play a sound
func playSound(play *Play, vc *discordgo.VoiceConnection) (err error) {
	log.WithFields(log.Fields{
		"play": play,
	}).Info("Playing sound")

	if vc == nil {
		vc, err = discord.ChannelVoiceJoin(play.GuildID, play.ChannelID, false, false)
		// vc.Receive = false
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Error("Failed to play sound")
			delete(queues, play.GuildID)
			return err
		}
	}

	// If we need to change channels, do that now
	if vc.ChannelID != play.ChannelID {
		vc.ChangeChannel(play.ChannelID, false, false)
		time.Sleep(time.Millisecond * 125)
	}

	// Track stats for this play in redis
	go trackSoundStats(play)

	// Sleep for a specified amount of time before playing the sound
	time.Sleep(time.Millisecond * 32)

	// Play the sound
	play.Sound.Play(vc)

	// If this is chained, play the chained sound
	if play.Next != nil {
		playSound(play.Next, vc)
	}

	// If there is another song in the queue, recurse and play that
	if len(queues[play.GuildID]) > 0 {
		play := <-queues[play.GuildID]
		playSound(play, vc)
		return nil
	}

	// If the queue is empty, delete it
	time.Sleep(time.Millisecond * time.Duration(play.Sound.PartDelay))
	delete(queues, play.GuildID)
	vc.Disconnect()
	return nil
}

func onReady(s *discordgo.Session, event *discordgo.Ready) {
	log.Info("Recieved READY payload")
	s.UpdateStatus(0, "github.com/noisemaster/airhornbot")
}

func onGuildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	if !event.Guild.Unavailable {
		return
	}

	for _, channel := range event.Guild.Channels {
		if channel.ID == event.Guild.ID {
			s.ChannelMessageSend(channel.ID, "**AIRHORN BOT READY FOR HORNING. TYPE `!AIRHORN` WHILE IN A VOICE CHANNEL TO ACTIVATE**")
			return
		}
	}
}

func scontains(key string, options ...string) bool {
	for _, item := range options {
		if item == key {
			return true
		}
	}
	return false
}

func calculateAirhornsPerSecond(cid string) {
	current, _ := strconv.Atoi(rcli.Get("airhorn:a:total").Val())
	time.Sleep(time.Second * 10)
	latest, _ := strconv.Atoi(rcli.Get("airhorn:a:total").Val())

	discord.ChannelMessageSend(cid, fmt.Sprintf("Current APS: %v", (float64(latest-current))/10.0))
}

func displayBotStats(cid string) {
	stats := runtime.MemStats{}
	runtime.ReadMemStats(&stats)

	users := 0
	for _, guild := range discord.State.Ready.Guilds {
		users += len(guild.Members)
	}

	w := &tabwriter.Writer{}
	buf := &bytes.Buffer{}

	w.Init(buf, 0, 4, 0, ' ', 0)
	fmt.Fprintf(w, "```\n")
	fmt.Fprintf(w, "Discordgo: \t%s\n", discordgo.VERSION)
	fmt.Fprintf(w, "Go: \t%s\n", runtime.Version())
	fmt.Fprintf(w, "Memory: \t%s / %s (%s total allocated)\n", humanize.Bytes(stats.Alloc), humanize.Bytes(stats.Sys), humanize.Bytes(stats.TotalAlloc))
	fmt.Fprintf(w, "Tasks: \t%d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "Servers: \t%d\n", len(discord.State.Ready.Guilds))
	fmt.Fprintf(w, "Users: \t%d\n", users)
	fmt.Fprintf(w, "```\n")
	w.Flush()
	discord.ChannelMessageSend(cid, buf.String())
}

func utilSumRedisKeys(keys []string) int {
	results := make([]*redis.StringCmd, 0)

	rcli.Pipelined(func(pipe *redis.Pipeline) error {
		for _, key := range keys {
			results = append(results, pipe.Get(key))
		}
		return nil
	})

	var total int
	for _, i := range results {
		t, _ := strconv.Atoi(i.Val())
		total += t
	}

	return total
}

func displayUserStats(cid, uid string) {
	keys, err := rcli.Keys(fmt.Sprintf("airhorn:*:user:%s:sound:*", uid)).Result()
	if err != nil {
		return
	}

	totalAirhorns := utilSumRedisKeys(keys)
	discord.ChannelMessageSend(cid, fmt.Sprintf("Total Airhorns: %v", totalAirhorns))
}

func displayServerStats(cid, sid string) {
	keys, err := rcli.Keys(fmt.Sprintf("airhorn:*:guild:%s:sound:*", sid)).Result()
	if err != nil {
		return
	}

	totalAirhorns := utilSumRedisKeys(keys)
	discord.ChannelMessageSend(cid, fmt.Sprintf("Total Airhorns: %v", totalAirhorns))
}

func utilGetMentioned(s *discordgo.Session, m *discordgo.MessageCreate) *discordgo.User {
	for _, mention := range m.Mentions {
		if mention.ID != s.State.Ready.User.ID {
			return mention
		}
	}
	return nil
}

func airhornBomb(cid string, guild *discordgo.Guild, user *discordgo.User, cs string) {
	count, _ := strconv.Atoi(cs)
	discord.ChannelMessageSend(cid, ":ok_hand:"+strings.Repeat(":trumpet:", count))

	// Cap it at something
	if count > 100 {
		return
	}

	play := createPlay(user, guild, AIRHORN, nil)
	vc, err := discord.ChannelVoiceJoin(play.GuildID, play.ChannelID, true, true)
	if err != nil {
		return
	}

	for i := 0; i < count; i++ {
		AIRHORN.Random().Play(vc)
	}

	vc.Disconnect()
}

// Handles bot operator messages, should be refactored (lmao)
func handleBotControlMessages(s *discordgo.Session, m *discordgo.MessageCreate, parts []string, g *discordgo.Guild) {
	if scontains(parts[1], "status") {
		displayBotStats(m.ChannelID)
	} else if scontains(parts[1], "stats") {
		if len(m.Mentions) >= 2 {
			displayUserStats(m.ChannelID, utilGetMentioned(s, m).ID)
		} else if len(parts) >= 3 {
			displayUserStats(m.ChannelID, parts[2])
		} else {
			displayServerStats(m.ChannelID, g.ID)
		}
	} else if scontains(parts[1], "bomb") && len(parts) >= 4 {
		airhornBomb(m.ChannelID, g, utilGetMentioned(s, m), parts[3])
	} else if scontains(parts[1], "aps") {
		s.ChannelMessageSend(m.ChannelID, ":ok_hand: give me a sec m8")
		go calculateAirhornsPerSecond(m.ChannelID)
	}
}

func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if len(m.Content) <= 0 || (m.Content[0] != '!' && len(m.Mentions) < 1) {
		return
	}

	msg := strings.Replace(m.ContentWithMentionsReplaced(), s.State.Ready.User.Username, "username", 1)
	parts := strings.Split(strings.ToLower(msg), " ")

	channel, _ := discord.State.Channel(m.ChannelID)
	if channel == nil {
		log.WithFields(log.Fields{
			"channel": m.ChannelID,
			"message": m.ID,
		}).Warning("Failed to grab channel")
		return
	}

	guild, _ := discord.State.Guild(channel.GuildID)
	if guild == nil {
		log.WithFields(log.Fields{
			"guild":   channel.GuildID,
			"channel": channel,
			"message": m.ID,
		}).Warning("Failed to grab guild")
		return
	}

	if strings.HasPrefix(strings.ToLower(m.Content), "!help") {
		messageLower := strings.ToLower(m.Content)
		helpCommand := strings.Split(messageLower, " ")
		if messageLower == "!help" || len(helpCommand) == 1 {
			s.ChannelMessageSend(m.ChannelID, "```xl\nAirhorn Basics\nThe airhorn bot has a few different sound effects that start with !\n\t- airhorn\n\t- anotha (or anothaone)\n\t- cena (or johncena)\n\t- ethan (or ethanbradberry, eb, h3h3)\n\t- stan (or stanislav)\n\t- bday (or birthday)\n\t- roode\n\t- revival\n\t- styles (or aj)\n\t- overwatch (or owult)\n\t- dummy\n\t- tobi (or tobiwan)\n\t- jones (or alexjones)\n\t- mummy\nFor more commands use !help {and any of the above}\n```")
		} else {
			if helpCommand[1] == "airhorn" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !airhorn {any of the below}\n\t- default\n\t- reverb\n\t- tripletap\n\t- fourtap\n\t- distant\n\t- echo\n\t- clownfull\n\t- clownshort\n\t- clownspam\n\t- highfartlong\n\t- highfartshort\n\t- midshort\n\t- truck\n```")
			} else if helpCommand[1] == "styles" || helpCommand[1] == "aj" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !styles (or !aj) {any of the below}\n\t- gay_community\n```")
			} else if helpCommand[1] == "anotha" || helpCommand[1] == "anothaone" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !anotha (or !anothaone) {any of the below}\n\t- one\n\t- one_classic\n\t- one_echo\n```")
			} else if helpCommand[1] == "stan" || helpCommand[1] == "stanislav" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !stan (or !stanislav) {any of the below}\n\t- herd\n\t- moo\n\t- x3\n```")
			} else if helpCommand[1] == "bday" || helpCommand[1] == "birthday" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !bday (or !birthday) {any of the below}\n\t- horn\n\t- horn3\n\t- sadhorn\n\t- weakhorn\n```")
			} else if helpCommand[1] == "roode" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !roode {any of the below}\n\t- glorious\n\t- defend\n\t- victorious_full\n```")
			} else if helpCommand[1] == "revival" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !revival {any of the below}\n\t- we_go_hard\n\t- say_yeah\n```")
			} else if helpCommand[1] == "ethan" || helpCommand[1] == "eb" || helpCommand[1] == "ethanbradberry" || helpCommand[1] == "h3h3" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !ethan (or eb, ethanbradberry, h3h3) {any of the below}\n\t- areyou_classic\n\t- areyou_condensed\n\t- areyou_crazy\n\t- areyou_ethan\n\t- classic\n\t- echo\n\t- high\n\t- slowandlown\n\t- cuts\n\t- beat\n\t- sodiepop\n```")
			} else if helpCommand[1] == "johncena" || helpCommand[1] == "cena" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !cena (or !johncena) {any of the below}\n\t- airhorn\n\t- echo\n\t- full\n\t- jc\n\t- nameis\n\t- spam\n```")
			} else if helpCommand[1] == "overwatch" || helpCommand[1] == "owult" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !overwatch (or !owult) {any of the below}\n\t- dva_enemy\n\t- genji_enemy\n\t- genji_friendly\n\t- hanzo_enemy\n\t- hanzo_friendly\n\t- junkrat_enemy\n\t- junkrat_friendly\n\t- lucio_friendly\n\t- lucio_enemy\n\t- mccree_enemy\n\t- mccree_friendly\n\t- mei_friendly\n\t- mei_enemy\n\t- pharah_enemy\n\t- reaper_friendly\n\t- 76_enemy\n\t- symmetra_friendly\n\t- torbjorn\n\t- tracer_enemy\n\t- tracer_friendly\n\t- widow_enemy\n\t- widow_friendly\n\t- zarya_enemy\n\t- zarya_enemy\n\t- zarya_friendly\n\t- zenyatta_enemy\n\t- dva_;)\n\t- anyong\nNote: Some ultimate sounds are missing (and as such are not listed here), so this command won't always play a sound\n```")
			} else if helpCommand[1] == "dummy" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !dummy {any of the below}\n\t- yeah\n```")
			} else if helpCommand[1] == "tobi" || helpCommand[1] == "tobiwan" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !tobi (or !tobiwan) {any of the below}\n\t- disaster\n```")
			} else if helpCommand[1] == "jones" || helpCommand[1] == "alexjones" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !jones (or !alexjones) {any of the below}\n\t- charging_goblins\n\t- in_bed_goblin\n\t- kissing_goblins\n\t- kissing_goblins_full\n\t- destroy_everything\n\t- gang_of_mustaches\n\t- have_children\n\t- human\n\t- pepsi_taste_test\n\t- sick_of_it\n\t- what_is_that_joke\n\t- what_is_venezula\n```")
			} else if helpCommand[1] == "mummy" {
				s.ChannelMessageSend(m.ChannelID, "```xl\nTo do play any of these sounds do !mummy (or !alexjones) {any of the below}\n\t- a number between 1 and 8\n```")
			}
		}
		return
	}

	// If this is a mention, it should come from the owner (otherwise we don't care)
	if len(m.Mentions) > 0 && m.Author.ID == OWNER && len(parts) > 0 {
		mentioned := false
		for _, mention := range m.Mentions {
			mentioned = (mention.ID == s.State.Ready.User.ID)
			if mentioned {
				break
			}
		}

		if mentioned {
			handleBotControlMessages(s, m, parts, guild)
		}
		return
	}

	// Find the collection for the command we got
	for _, coll := range COLLECTIONS {
		if scontains(parts[0], coll.Commands...) {

			// If they passed a specific sound effect, find and select that (otherwise play nothing)
			var sound *Sound
			if len(parts) > 1 {
				for _, s := range coll.Sounds {
					if parts[1] == s.Name {
						sound = s
					}
				}

				if sound == nil {
					return
				}
			}

			go enqueuePlay(m.Author, guild, coll, sound)
			return
		}
	}
}

func main() {
	var (
		Token      = flag.String("t", "", "Discord Authentication Token")
		Redis      = flag.String("r", "", "Redis Connection String")
		Shard      = flag.String("s", "", "Shard ID")
		ShardCount = flag.String("c", "", "Number of shards")
		Owner      = flag.String("o", "", "Owner ID")
		err        error
	)
	flag.Parse()

	if *Owner != "" {
		OWNER = *Owner
	}

	// Preload all the sounds
	log.Info("Preloading sounds...")
	for _, coll := range COLLECTIONS {
		coll.Load()
	}

	// If we got passed a redis server, try to connect
	if *Redis != "" {
		log.Info("Connecting to redis...")
		rcli = redis.NewClient(&redis.Options{Addr: *Redis, DB: 0})
		_, err = rcli.Ping().Result()

		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Fatal("Failed to connect to redis")
			return
		}
	}

	// Create a discord session
	log.Info("Starting discord session...")
	discord, err = discordgo.New(*Token)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to create discord session")
		return
	}

	// Set sharding info
	discord.ShardID, _ = strconv.Atoi(*Shard)
	discord.ShardCount, _ = strconv.Atoi(*ShardCount)

	if discord.ShardCount <= 0 {
		discord.ShardCount = 1
	}

	discord.AddHandler(onReady)
	discord.AddHandler(onGuildCreate)
	discord.AddHandler(onMessageCreate)

	err = discord.Open()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to create discord websocket connection")
		return
	}

	// We're running!
	log.Info("AIRHORNBOT is ready to horn it up.")

	// Wait for a signal to quit
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c
}
